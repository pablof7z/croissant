package global

import "testing"

func TestLoadSettingsUsesExpectedGroupCreateBurstLimit(t *testing.T) {
	settings, err := loadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}

	if got, want := settings.Groups.CreateGroupRateLimit.MaxTokens, 100; got != want {
		t.Fatalf("group create max tokens = %d, want %d", got, want)
	}
}
