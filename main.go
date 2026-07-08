package main

import (
	"embed"
	"net"
	"net/http"
	_ "net/http/pprof"

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
	store          eventstore.Store
	L              = global.L
	pool           = nostr.NewPool()
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

	var err error
	mmmm, store, err = initStore(global.E.DataPath)
	if err != nil {
		L.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer mmmm.Close()

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
		L.Fatal().Err(err).Msg("failed to initialize relay")
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
	L.Printf("listening on http://%s", addr)
	handler := requestLogMiddleware(loggedUserMiddleware(relayHandler))
	if err := http.ListenAndServe(addr, handler); err != nil {
		L.Fatal().Err(err).Msg("server error")
	}
}
