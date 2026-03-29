package main

import (
	"context"
	"net"
	"net/http"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/croissant/groups"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

var (
	log            = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	currentVersion string
	S              Env
	relay          *khatru.Relay
	settingsState  *SettingsState
)

type Env struct {
	Host     string `envconfig:"HOST" default:"127.0.0.1"`
	Port     string `envconfig:"PORT" default:"9888"`
	DataPath string `envconfig:"DATAPATH" default:"data"`
}

func main() {
	err := envconfig.Process("", &S)
	if err != nil {
		log.Fatal().Err(err).Msg("error loading environment configuration")
	}

	settings, err := loadSettings(S.DataPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load settings")
	}

	settingsState = &SettingsState{settings: settings}

	manager, store, err := initStore(S.DataPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer manager.Close()

	relayBaseURL := relayBaseURL(settings, S.Host, S.Port)

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

	relayURL := relayWSURL(settings, S.Host, S.Port)

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
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		settingsState.mu.RLock()
		currentSettings := settingsState.settings
		settingsState.mu.RUnlock()

		loggedPubKey, ok := getLoggedUser(r, currentSettings)
		isOwner := ok && currentSettings.OwnerPubKey != nostr.ZeroPK && loggedPubKey == currentSettings.OwnerPubKey
		if err := Home(currentSettings, isOwner).Render(r.Context(), w); err != nil {
			log.Error().Err(err).Msg("failed to render home")
		}
	})
	mux.HandleFunc("GET /settings", settingsHandler)

	addr := net.JoinHostPort(S.Host, S.Port)
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, relay); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}

func relayBaseURL(settings Settings, host string, port string) string {
	if settings.Domain != "" {
		return settings.HTTPScheme() + settings.Domain
	}

	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func relayWSURL(settings Settings, host string, port string) string {
	if settings.Domain != "" {
		return settings.WSScheme() + settings.Domain
	}

	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return "ws://" + net.JoinHostPort(host, port)
}
