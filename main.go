package main

import (
	"context"
	"embed"
	"net"
	"net/http"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/croissant/groups"
)

//go:embed static
var staticFiles embed.FS

var (
	currentVersion string
	store          *mmm.IndexingLayer
	L              = global.L
)

func main() {
	global.Init()

	manager, storeInstance, err := initStore(global.E.DataPath)
	store = storeInstance
	if err != nil {
		L.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer manager.Close()

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

	global.R.UseEventstore(store, 1000)

	relayURL := global.S.RelayWSURL(global.E.Host, global.E.Port)

	groups.Init(groups.Options{
		DB:        store,
		SecretKey: global.S.RelaySecretKey,
		Broadcast: global.R.BroadcastEvent,
		RelayURL:  relayURL,
		BaseURL:   relayBaseURL,
		LiveKit: groups.LiveKitSettings{
			ServerURL: global.S.Groups.LiveKitServerURL,
			APIKey:    global.S.Groups.LiveKitAPIKey,
			APISecret: global.S.Groups.LiveKitAPISecret,
		},
	})

	global.R.OnEvent = func(ctx context.Context, event nostr.Event) (bool, string) {
		if groups.IsGroupEvent(event) {
			return groups.State.RejectEvent(ctx, event)
		}
		return true, "blocked: not a group event"
	}
	global.R.OnEventSaved = func(ctx context.Context, event nostr.Event) {
		if groups.IsGroupEvent(event) {
			groups.State.HandleEventSaved(event)
		}
	}

	mux := global.R.Router()
	groups.SetupHTTP(mux)
	mux.HandleFunc("GET /favicon.ico", faviconHandler)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("POST /settings", global.SettingsHandler)
	mux.HandleFunc("GET /group/{id}", groupHandler)
	mux.HandleFunc("GET /", homeHandler)

	addr := net.JoinHostPort(global.E.Host, global.E.Port)
	L.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, global.R); err != nil {
		L.Fatal().Err(err).Msg("server error")
	}
}
