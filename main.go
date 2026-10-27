package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"

	"fiatjaf.com/croissant/global"
)

//go:embed static
var staticFiles embed.FS

var (
	currentVersion string
	mmmm           *mmm.MultiMmapManager
	mmmGate        *storeGate
	store          eventstore.Store
	L              = global.L
	pool           = nostr.NewPool()
)

// shutdownTimeout bounds how long SIGTERM/SIGINT waits for plain HTTP requests
// to drain. storeCloseTimeout then bounds the wait for store operations still
// running (websocket handlers included) before mmm is closed.
const (
	shutdownTimeout   = 5 * time.Second
	storeCloseTimeout = 10 * time.Second
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			logHTTPUpgrade(r.RemoteAddr, r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		logHTTPRequest(r.RemoteAddr, r.Method, r.URL.Path, rec.status)
	})
}

func loggedUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		ctx := global.WithLoggedUser(r.Context(), loggedUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	global.Init()

	if err := run(); err != nil {
		L.Fatal().Err(err).Msg("croissant stopped")
	}
}

// run serves until SIGTERM/SIGINT and returns only after the store is closed.
// Closing the store matters: on macOS LMDB keeps its locks in POSIX named
// semaphores and unlinks them only in a clean mdb_env_close, so a relay that
// dies without closing leaks them until kern.posix.sem.max is exhausted.
//
// Shutdown order: stop accepting and drain plain HTTP, close the hijacked
// websocket connections (Shutdown ignores them), wait for every store
// operation still in flight, then close mmm. Closing mmm under a running
// operation would touch unmapped memory.
func run() (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mmmm, mmmGate, store, err = initStore(global.E.DataPath)
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}
	defer func() {
		err = errors.Join(err, closeStore(mmmm, mmmGate, storeCloseTimeout))
	}()

	if err := initEventLog(); err != nil {
		L.Warn().Err(err).Msg("event log disabled")
	}

	relayBaseURL := global.S.RelayBaseURL()
	relayURL := global.S.RelayWSURL()
	relay := khatru.NewRelay()

	relay.Info.Software = "https://viewsource.win/fiatjaf.com/croissant"

	State = NewGroupsState(Options{
		DB:        store,
		SecretKey: global.S.RelaySecretKey,
		RelayURL:  relayURL,
		BaseURL:   relayBaseURL,
		LiveKit: LiveKitSettings{
			ServerURL: global.S.Groups.LiveKitServerURL,
			APIKey:    global.S.Groups.LiveKitAPIKey,
			APISecret: global.S.Groups.LiveKitAPISecret,
		},
	})
	if err := configureRelay(relay, relayBaseURL); err != nil {
		return fmt.Errorf("failed to initialize relay: %w", err)
	}

	global.R = relay
	relayHandler := &relayHandler{}
	relayHandler.Set(relay)
	global.ResetRelay = func() error {
		return resetRelay(relayHandler)
	}

	go func() {
		if err := http.ListenAndServe("127.0.0.1:3337", nil); err != nil {
			L.Error().Err(err).Msg("pprof server error")
		}
	}()

	addr := net.JoinHostPort(global.E.Host, global.E.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	hijacked := &hijackedConns{}
	server := &http.Server{
		Handler:   requestLogMiddleware(loggedUserMiddleware(relayHandler)),
		ConnState: hijacked.ConnState,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(hijacked.Listener(ln)) }()
	L.Printf("listening on http://%s", addr)

	select {
	case err := <-serveErr:
		hijacked.CloseAll()
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}
	stop() // a second signal kills the process immediately

	L.Info().Msg("shutting down")
	return shutdownServer(server, hijacked, shutdownTimeout)
}

// shutdownServer drains plain HTTP requests, then closes the hijacked
// websocket connections so their handlers end and clients reconnect elsewhere.
// A drain that times out is reported as an error so the process exits
// non-zero instead of claiming a clean stop.
func shutdownServer(server *http.Server, hijacked *hijackedConns, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if n := hijacked.CloseAll(); n > 0 {
		L.Info().Int("connections", n).Msg("closed websocket connections")
	}
	if shutdownErr != nil {
		return fmt.Errorf("http shutdown incomplete: %w", shutdownErr)
	}
	return nil
}
