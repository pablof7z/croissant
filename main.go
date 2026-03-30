package main

import (
	"context"
	"embed"
	"net"
	"net/http"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/croissant/groups"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

//go:embed static
var staticFiles embed.FS

var (
	L              = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	currentVersion string
	S              Env
	relay          *khatru.Relay
	store          *mmm.IndexingLayer
)

type Env struct {
	Host     string `envconfig:"HOST" default:"127.0.0.1"`
	Port     string `envconfig:"PORT" default:"9888"`
	DataPath string `envconfig:"DATAPATH" default:"data"`
}

func main() {
	err := envconfig.Process("", &S)
	if err != nil {
		L.Fatal().Err(err).Msg("error loading environment configuration")
	}

	settings, err = loadSettings(S.DataPath)
	if err != nil {
		L.Fatal().Err(err).Msg("failed to load settings")
	}

	manager, storeInstance, err := initStore(S.DataPath)
	store = storeInstance
	if err != nil {
		L.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer manager.Close()

	relayBaseURL := settings.relayBaseURL(S.Host, S.Port)

	relay = khatru.NewRelay()
	relay.ServiceURL = relayBaseURL
	relay.Info.Name = settings.RelayName
	relay.Info.Description = settings.RelayDescription
	relay.Info.Contact = settings.RelayContact
	relay.Info.Icon = settings.RelayIcon
	pk := settings.RelaySecretKey.Public()
	relay.Info.PubKey = &pk
	relay.Info.Self = &pk
	relay.Info.AddSupportedNIP(29)

	relay.UseEventstore(store, 1000)

	relayURL := settings.relayWSURL(S.Host, S.Port)

	groups.Init(groups.Options{
		DB:        store,
		SecretKey: settings.RelaySecretKey,
		Broadcast: relay.BroadcastEvent,
		RelayURL:  relayURL,
		BaseURL:   relayBaseURL,
		LiveKit: groups.LiveKitSettings{
			ServerURL: settings.Groups.LiveKitServerURL,
			APIKey:    settings.Groups.LiveKitAPIKey,
			APISecret: settings.Groups.LiveKitAPISecret,
		},
	})

	relay.OnEvent = func(ctx context.Context, event nostr.Event) (bool, string) {
		if groups.IsGroupEvent(event) {
			return groups.State.RejectEvent(ctx, event)
		}
		return true, "blocked: not a group event"
	}
	relay.OnEventSaved = func(ctx context.Context, event nostr.Event) {
		if groups.IsGroupEvent(event) {
			groups.State.HandleEventSaved(event)
		}
	}

	mux := relay.Router()
	groups.SetupHTTP(mux)
	mux.HandleFunc("GET /favicon.ico", faviconHandler)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("POST /settings", settingsHandler)
	mux.HandleFunc("GET /", homeHandler)

	addr := net.JoinHostPort(S.Host, S.Port)
	L.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, relay); err != nil {
		L.Fatal().Err(err).Msg("server error")
	}
}
