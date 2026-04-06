package main

import (
	"context"
	"embed"
	"net"
	"net/http"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"github.com/pemistahl/lingua-go"

	"fiatjaf.com/croissant/global"
)

//go:embed static
var staticFiles embed.FS

var (
	currentVersion string
	mmmm           *mmm.MultiMmapManager
	store          *mmm.IndexingLayer
	L              = global.L
	pool           = nostr.NewPool(nostr.PoolOptions{})
)

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

	detector = lingua.NewLanguageDetectorBuilder().FromLanguages(lingua.AllSpokenLanguages()...).Build()

	relayBaseURL := global.S.RelayBaseURL(global.E.Host, global.E.Port)

	global.R = khatru.NewRelay()
	global.R.ServiceURL = relayBaseURL
	global.R.Info.Name = global.S.RelayName
	global.R.Info.Description = global.S.RelayDescription
	global.R.Info.Contact = global.S.RelayContact
	global.R.Info.Icon = global.S.RelayIcon
	pk := global.S.RelaySecretKey.Public()
	global.R.Info.PubKey = &pk
	global.R.Info.Self = &pk
	global.R.Info.AddSupportedNIP(29)

	relayURL := global.S.RelayWSURL(global.E.Host, global.E.Port)

	State = NewGroupsState(Options{
		DB:        store,
		SecretKey: global.S.RelaySecretKey,
		Broadcast: global.R.BroadcastEvent,
		RelayURL:  relayURL,
		BaseURL:   relayBaseURL,
		LiveKit: LiveKitSettings{
			ServerURL: global.S.Groups.LiveKitServerURL,
			APIKey:    global.S.Groups.LiveKitAPIKey,
			APISecret: global.S.Groups.LiveKitAPISecret,
		},
	})

	global.R.QueryStored = State.Query
	global.R.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		return store.SaveEvent(event)
	}
	global.R.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		return store.ReplaceEvent(event)
	}
	global.R.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		return store.DeleteEvent(id)
	}

	global.R.OnEvent = State.RejectEvent
	global.R.OnEventSaved = State.HandleEventSaved
	global.R.OnRequest = State.RequestAuthWhenNecessary
	global.R.PreventBroadcast = State.ShouldPreventBroadcast

	mux := global.R.Router()

	// basic routes
	mux.HandleFunc("GET /favicon.ico", faviconHandler)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("POST /settings", global.SettingsHandler)

	// group page
	mux.HandleFunc("GET /group/{id}", groupHandler)

	// nip29 livekit
	mux.HandleFunc("GET /.well-known/nip29/livekit", livekitStatusHandler)
	mux.HandleFunc("GET /.well-known/nip29/livekit/{groupId}", livekitAuthHandler)
	mux.HandleFunc("POST /groups/livekit/webhook", livekitWebhookHandler)

	// home
	mux.HandleFunc("GET /", homeHandler)

	if global.S.Blossom.Enabled {
		if err := initBlossom(global.R, relayBaseURL); err != nil {
			L.Fatal().Err(err).Msg("failed to initialize blossom")
		}
	}

	addr := net.JoinHostPort(global.E.Host, global.E.Port)
	L.Printf("listening on http://%s", addr)
	handler := loggedUserMiddleware(global.R)
	if err := http.ListenAndServe(addr, handler); err != nil {
		L.Fatal().Err(err).Msg("server error")
	}
}
