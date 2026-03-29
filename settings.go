package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
)

type Settings struct {
	Domain           string          `json:"domain"`
	RelayName        string          `json:"relay_name"`
	RelayDescription string          `json:"relay_description"`
	RelayContact     string          `json:"relay_contact"`
	RelayIcon        string          `json:"relay_icon"`
	RelaySecretKey   nostr.SecretKey `json:"relay_secret_key"`

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
			RelayName:        "relay29",
			RelayDescription: "nostr relay",
			RelayIcon:        "",
			RelaySecretKey:   nostr.Generate(),
		}

		if err := saveSettings(dataPath, settings); err != nil {
			return Settings{}, err
		}

		return settings, nil
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("failed to parse settings: %w", err)
	}

	if settings.RelaySecretKey == nostr.ZeroSecretKey {
		settings.RelaySecretKey = nostr.Generate()
		if err := saveSettings(dataPath, settings); err != nil {
			return Settings{}, err
		}
	}

	return settings, nil
}

func saveSettings(dataPath string, settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := os.WriteFile(settingsPath(dataPath), data, 0644); err != nil {
		return fmt.Errorf("failed to write settings: %w", err)
	}

	return nil
}
