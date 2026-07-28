package blossom

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/mailru/easyjson"
)

func readAuthorization(r *http.Request) (*nostr.Event, error) {
	token := r.Header.Get("Authorization")
	if !strings.HasPrefix(token, "Nostr ") {
		return nil, nil
	}

	eventj, err := decodeAuthorizationToken(token[6:])
	if err != nil {
		return nil, fmt.Errorf("invalid base64 token")
	}
	var evt nostr.Event
	if err := easyjson.Unmarshal(eventj, &evt); err != nil {
		return nil, fmt.Errorf("broken event")
	}
	if evt.Kind != 24242 || !evt.CheckID() {
		return nil, fmt.Errorf("invalid event")
	}
	if !evt.VerifySignature() {
		return nil, fmt.Errorf("invalid signature")
	}

	expirationTag := evt.Tags.Find("expiration")
	if expirationTag == nil {
		return nil, fmt.Errorf("missing \"expiration\" tag")
	}
	expiration, _ := strconv.ParseInt(expirationTag[1], 10, 64)
	if nostr.Timestamp(expiration) < nostr.Now() {
		return nil, fmt.Errorf("event expired")
	}

	return &evt, nil
}

func decodeAuthorizationToken(token string) ([]byte, error) {
	eventj, err := base64.RawURLEncoding.DecodeString(token)
	if err == nil {
		return eventj, nil
	}

	// Deployed clients also use padded standard Base64. Accept both encodings;
	// the decoded event still passes the same ID, signature, and expiry checks.
	return base64.StdEncoding.DecodeString(token)
}
