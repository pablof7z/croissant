package global

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
)

var S Settings

type Settings struct {
	Domain           string          `json:"domain"`
	RelayName        string          `json:"relay_name"`
	RelayDescription string          `json:"relay_description"`
	RelayContact     string          `json:"relay_contact"`
	RelayIcon        string          `json:"relay_icon"`
	RelaySecretKey   nostr.SecretKey `json:"relay_secret_key"`
	OwnerPubKey      nostr.PubKey    `json:"owner_pubkey"`

	Groups struct {
		LiveKitServerURL string `json:"livekit_server_url"`
		LiveKitAPIKey    string `json:"livekit_apikey"`
		LiveKitAPISecret string `json:"livekit_apisecret"`
	} `json:"groups"`
}

func (s Settings) HTTPScheme() string {
	if s.Domain == "" {
		return "http://"
	}
	if strings.HasPrefix(s.Domain, "127.0.0.1") ||
		strings.HasPrefix(s.Domain, "0.0.0.0") ||
		strings.HasPrefix(s.Domain, "localhost") {
		return "http://"
	}
	return "https://"
}

func (s Settings) WSScheme() string {
	return "ws" + s.HTTPScheme()[4:]
}

func (s Settings) RelayBaseURL(host string, port string) string {
	if s.Domain != "" {
		return s.HTTPScheme() + s.Domain
	}

	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func (s Settings) RelayWSURL(host string, port string) string {
	if s.Domain != "" {
		return s.WSScheme() + s.Domain
	}

	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return "ws://" + net.JoinHostPort(host, port)
}

func settingsPath(dataPath string) string {
	return filepath.Join(dataPath, "settings.json")
}

func loadSettings(dataPath string) (Settings, error) {
	path := settingsPath(dataPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Settings{}, fmt.Errorf("failed to create settings dir: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Settings{}, fmt.Errorf("failed to read settings: %w", err)
		}

		settings := Settings{
			RelayName:        "croissant",
			RelayDescription: "groups provider",
			RelayIcon:        "",
			RelaySecretKey:   nostr.Generate(),
		}

		if err := settings.save(dataPath); err != nil {
			return Settings{}, err
		}

		return settings, nil
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("failed to parse settings: %w", err)
	}

	if settings.RelaySecretKey == [32]byte{} {
		settings.RelaySecretKey = nostr.Generate()
		if err := settings.save(dataPath); err != nil {
			return Settings{}, err
		}
	}

	if settings.OwnerPubKey == nostr.ZeroPK {
		settings.OwnerPubKey = settings.RelaySecretKey.Public()
		if err := settings.save(dataPath); err != nil {
			return Settings{}, err
		}
	}

	return settings, nil
}

func (settings Settings) save(dataPath string) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := os.WriteFile(settingsPath(dataPath), data, 0644); err != nil {
		return fmt.Errorf("failed to write settings: %w", err)
	}

	return nil
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	loggedPubKey, ok := GetLoggedUser(r)
	if !ok || loggedPubKey != S.OwnerPubKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	updated := S
	updated.RelayName = strings.TrimSpace(r.FormValue("relay_name"))
	updated.RelayDescription = strings.TrimSpace(r.FormValue("relay_description"))
	updated.RelayContact = strings.TrimSpace(r.FormValue("relay_contact"))
	updated.RelayIcon = strings.TrimSpace(r.FormValue("relay_icon"))

	if ownerInput := strings.TrimSpace(r.FormValue("owner_pubkey")); ownerInput != "" {
		if pk, ok := pubKeyFromInput(ownerInput); ok {
			updated.OwnerPubKey = pk
		} else {
			http.Error(w, "invalid owner pubkey", http.StatusBadRequest)
			return
		}
	}

	if err := S.save(E.DataPath); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	S = updated
	R.Info.Name = updated.RelayName
	R.Info.Description = updated.RelayDescription
	R.Info.Contact = updated.RelayContact
	R.Info.Icon = updated.RelayIcon

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
