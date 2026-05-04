package main

import (
	"embed"
	"net"
	"net/http"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/eventstore/bleve"
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
	store          eventstore.Store
	L              = global.L
	pool           = nostr.NewPool()

	GlobalSearchIndex *bleve.BleveBackend
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

	detector = lingua.NewLanguageDetectorBuilder().FromLanguages(bleve.SupportedLanguages...).Build()

	relayBaseURL := global.S.RelayBaseURL()
	relayURL := global.S.RelayWSURL()
	relay := khatru.NewRelay()

	GlobalSearchIndex = &bleve.BleveBackend{
		Path:           filepath.Join(global.E.DataPath, "global-search"),
		Languages:      []lingua.Language{lingua.English},
		IndexableKinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		RawEventStore:  store,
	}
	if err := GlobalSearchIndex.Init(); err != nil {
		L.Fatal().Err(err).Msg("failed to initialize global search")
	}

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

	addr := net.JoinHostPort(global.E.Host, global.E.Port)
	L.Printf("listening on http://%s", addr)
	handler := loggedUserMiddleware(relayHandler)
	if err := http.ListenAndServe(addr, handler); err != nil {
		L.Fatal().Err(err).Msg("server error")
	}
}
