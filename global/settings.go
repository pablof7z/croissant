package global

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
		LiveKitServerURL          string   `json:"livekit_server_url"`
		LiveKitAPIKey             string   `json:"livekit_apikey"`
		LiveKitAPISecret          string   `json:"livekit_apisecret"`
		CreateGroupPresenceRelays []string `json:"create_group_presence_relays"`
		FreeTransitPresenceRelays []string `json:"free_transit_presence_relays"`
		CreateGroupRateLimit      struct {
			TokensPerInterval int `json:"tokens_per_interval"`
			IntervalSeconds   int `json:"interval_seconds"`
			MaxTokens         int `json:"max_tokens"`
		} `json:"create_group_rate_limit"`
	} `json:"groups"`

	relayPublicKey nostr.PubKey
}

func (s Settings) RelayPublicKey() nostr.PubKey {
	if s.relayPublicKey == nostr.ZeroPK {
		s.relayPublicKey = s.RelaySecretKey.Public()
	}
	return s.relayPublicKey
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
		settings.Groups.CreateGroupRateLimit.TokensPerInterval = 1
		settings.Groups.CreateGroupRateLimit.IntervalSeconds = 10800
		settings.Groups.CreateGroupRateLimit.MaxTokens = 3

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

	parseCSV := func(field string) []string {
		value := strings.TrimSpace(r.FormValue(field))
		if value == "" {
			return nil
		}
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			result = append(result, trimmed)
		}
		return result
	}

	parseInt := func(field string, current int) (int, error) {
		value := strings.TrimSpace(r.FormValue(field))
		if value == "" {
			return current, nil
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("invalid %s", field)
		}
		if parsed < 0 {
			return 0, fmt.Errorf("invalid %s", field)
		}
		return parsed, nil
	}

	if tokens, err := parseInt("group_create_rate_tokens_per_interval", updated.Groups.CreateGroupRateLimit.TokensPerInterval); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else {
		updated.Groups.CreateGroupRateLimit.TokensPerInterval = tokens
	}
	if intervalSeconds, err := parseInt("group_create_rate_interval_seconds", updated.Groups.CreateGroupRateLimit.IntervalSeconds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else {
		updated.Groups.CreateGroupRateLimit.IntervalSeconds = intervalSeconds
	}
	if maxTokens, err := parseInt("group_create_rate_max_tokens", updated.Groups.CreateGroupRateLimit.MaxTokens); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else {
		updated.Groups.CreateGroupRateLimit.MaxTokens = maxTokens
	}

	updated.Groups.CreateGroupPresenceRelays = parseCSV("create_group_presence_relays")
	updated.Groups.FreeTransitPresenceRelays = parseCSV("free_transit_presence_relays")

	if err := updated.save(E.DataPath); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	S = updated
	ConfigureGroupCreateRateLimit(S)
	R.Info.Name = updated.RelayName
	R.Info.Description = updated.RelayDescription
	R.Info.Contact = updated.RelayContact
	R.Info.Icon = updated.RelayIcon

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
