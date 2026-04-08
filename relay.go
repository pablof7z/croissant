package main

import (
	"context"
	"net/http"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"

	"fiatjaf.com/croissant/global"
)

type relayHandler struct {
	current atomic.Value
}

func (h *relayHandler) Set(relay *khatru.Relay) {
	h.current.Store(relay)
}

func (h *relayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	relay, ok := h.current.Load().(*khatru.Relay)
	if !ok || relay == nil {
		http.Error(w, "relay not ready", http.StatusServiceUnavailable)
		return
	}
	relay.ServeHTTP(w, r)
}

func configureRelay(relay *khatru.Relay, relayBaseURL string) error {
	relay.ServiceURL = relayBaseURL
	relay.Info.Name = global.S.RelayName
	relay.Info.Description = global.S.RelayDescription
	relay.Info.Contact = global.S.RelayContact
	relay.Info.Icon = global.S.RelayIcon
	pk := global.S.RelaySecretKey.Public()
	relay.Info.PubKey = &pk
	relay.Info.Self = &pk
	relay.Info.AddSupportedNIP(29)

	relay.QueryStored = State.Query
	relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		return store.SaveEvent(event)
	}
	relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		return store.ReplaceEvent(event)
	}
	relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		return store.DeleteEvent(id)
	}

	relay.OnEvent = State.RejectEvent
	relay.OnEventSaved = State.HandleEventSaved
	relay.OnRequest = State.RequestAuthWhenNecessary
	relay.PreventBroadcast = State.ShouldPreventBroadcast

	mux := relay.Router()

	// basic routes
	mux.HandleFunc("GET /favicon.ico", faviconHandler)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("POST /settings", global.SettingsHandler)
	mux.HandleFunc("POST /group/{id}/wipeout", wipeGroupHandler)

	// group page
	mux.HandleFunc("GET /group/{id}", groupHandler)

	// nip29 livekit
	mux.HandleFunc("GET /.well-known/nip29/livekit", livekitStatusHandler)
	mux.HandleFunc("GET /.well-known/nip29/livekit/{groupId}", livekitAuthHandler)
	mux.HandleFunc("POST /groups/livekit/webhook", livekitWebhookHandler)

	// home
	mux.HandleFunc("GET /", homeHandler)

	if global.S.Blossom.Enabled {
		if err := initBlossom(relay, relayBaseURL); err != nil {
			return err
		}
	} else {
		resetBlossom()
	}

	return nil
}

func resetRelay(handler *relayHandler) error {
	relayBaseURL := global.S.RelayBaseURL()
	relayURL := global.S.RelayWSURL()

	relay := khatru.NewRelay()
	State.UpdateRuntimeConfig(relayURL, relayBaseURL, LiveKitSettings{
		ServerURL: global.S.Groups.LiveKitServerURL,
		APIKey:    global.S.Groups.LiveKitAPIKey,
		APISecret: global.S.Groups.LiveKitAPISecret,
	})

	if err := configureRelay(relay, relayBaseURL); err != nil {
		return err
	}

	global.R = relay
	handler.Set(relay)
	return nil
}
