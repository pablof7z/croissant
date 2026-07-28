package blossom

import (
	"encoding/base64"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/mailru/easyjson"
)

func TestReadAuthorizationAcceptsBUD11Base64URLWithoutPadding(t *testing.T) {
	event, eventJSON := signedAuthorizationEvent(t)
	token := base64.RawURLEncoding.EncodeToString(eventJSON)
	if strings.Contains(token, "=") {
		t.Fatalf("base64url token unexpectedly contains padding: %q", token)
	}
	if !strings.ContainsAny(token, "-_") {
		t.Fatalf("test token does not exercise the URL-safe alphabet: %q", token)
	}

	request := httptest.NewRequest("PUT", "/upload", nil)
	request.Header.Set("Authorization", "Nostr "+token)

	got, err := readAuthorization(request)
	if err != nil {
		t.Fatalf("readAuthorization rejected BUD-11 token: %v", err)
	}
	if got == nil {
		t.Fatal("readAuthorization returned no event")
	}
	if !reflect.DeepEqual(*got, event) {
		t.Fatalf("readAuthorization event mismatch:\n got: %#v\nwant: %#v", *got, event)
	}
}

func TestReadAuthorizationKeepsAcceptingLegacyStandardBase64(t *testing.T) {
	event, eventJSON := signedAuthorizationEvent(t)
	request := httptest.NewRequest("PUT", "/upload", nil)
	request.Header.Set(
		"Authorization",
		"Nostr "+base64.StdEncoding.EncodeToString(eventJSON),
	)

	got, err := readAuthorization(request)
	if err != nil {
		t.Fatalf("readAuthorization rejected legacy token: %v", err)
	}
	if got == nil || !reflect.DeepEqual(*got, event) {
		t.Fatalf("readAuthorization event mismatch: got %#v, want %#v", got, event)
	}
}

func TestReadAuthorizationRejectsMalformedBase64(t *testing.T) {
	request := httptest.NewRequest("PUT", "/upload", nil)
	request.Header.Set("Authorization", "Nostr definitely-not-*-base64")

	got, err := readAuthorization(request)
	if got != nil {
		t.Fatalf("readAuthorization returned an event for malformed input: %#v", got)
	}
	if err == nil || err.Error() != "invalid base64 token" {
		t.Fatalf("readAuthorization error = %v, want invalid base64 token", err)
	}
}

func signedAuthorizationEvent(t *testing.T) (nostr.Event, []byte) {
	t.Helper()

	secretKey := nostr.MustSecretKeyFromHex(
		"0000000000000000000000000000000000000000000000000000000000000001",
	)
	event := nostr.Event{
		PubKey:    secretKey.Public(),
		CreatedAt: nostr.Timestamp(1_785_000_000),
		Kind:      nostr.Kind(24242),
		Tags: nostr.Tags{
			{"t", "upload"},
			{"expiration", "2000000000"},
		},
		Content: "Upload blob 🧁",
	}
	if err := event.Sign([32]byte(secretKey)); err != nil {
		t.Fatalf("sign authorization event: %v", err)
	}

	eventJSON, err := easyjson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal authorization event: %v", err)
	}
	return event, eventJSON
}
