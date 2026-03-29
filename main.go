package main

import (
	"context"
	"net"
	"net/http"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/relay29/groups"
	"github.com/rs/zerolog"
)

var log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

type envConfig struct {
	Host     string
	Port     string
	DataPath string
}

func loadEnv() envConfig {
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3334"
	}
	dataPath := os.Getenv("DATA_PATH")
	if dataPath == "" {
		dataPath = "./data"
	}

	return envConfig{Host: host, Port: port, DataPath: dataPath}
}

func main() {
	cfg := loadEnv()

	settings, err := loadSettings(cfg.DataPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load settings")
	}

	manager, store, err := initStore(cfg.DataPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer manager.Close()

	relayBaseURL := relayBaseURL(settings, cfg.Host, cfg.Port)

	relay := khatru.NewRelay()
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

	relayURL := relayWSURL(settings, cfg.Host, cfg.Port)

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

	addr := net.JoinHostPort(cfg.Host, cfg.Port)
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
