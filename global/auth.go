package global

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

const nip98CookieName = "nip98"

type loggedUserContextKey struct{}

func GetLoggedUser(r *http.Request) (nostr.PubKey, bool) {
	cookie, err := r.Cookie(nip98CookieName)
	if err != nil || cookie == nil || cookie.Value == "" {
		return nostr.ZeroPK, false
	}

	evtJSON, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nostr.ZeroPK, false
	}

	var evt nostr.Event
	if err := json.Unmarshal(evtJSON, &evt); err != nil {
		return nostr.ZeroPK, false
	}

	if evt.Kind != 27235 {
		return nostr.ZeroPK, false
	}

	domainTag := evt.Tags.Find("domain")
	if domainTag == nil || len(domainTag) < 2 {
		return nostr.ZeroPK, false
	}

	expectedDomain := E.Domain
	if expectedDomain == "" {
		expectedDomain = r.Host
	}

	if domainTag[1] != expectedDomain {
		return nostr.ZeroPK, false
	}

	if !evt.VerifySignature() {
		return nostr.ZeroPK, false
	}

	return evt.PubKey, true
}

func WithLoggedUser(ctx context.Context, pubKey nostr.PubKey) context.Context {
	return context.WithValue(ctx, loggedUserContextKey{}, pubKey)
}

func LoggedUserFromContext(ctx context.Context) nostr.PubKey {
	if ctx == nil {
		return nostr.ZeroPK
	}

	value := ctx.Value(loggedUserContextKey{})
	if value == nil {
		return nostr.ZeroPK
	}

	pubKey, ok := value.(nostr.PubKey)
	if !ok {
		return nostr.ZeroPK
	}

	return pubKey
}

func pubKeyFromInput(input string) (nostr.PubKey, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nostr.ZeroPK, false
	}

	if prefix, value, err := nip19.Decode(input); err == nil {
		switch prefix {
		case "npub":
			return value.(nostr.PubKey), true
		case "nprofile":
			return value.(nostr.ProfilePointer).PublicKey, true
		}
	}

	if pk, err := nostr.PubKeyFromHex(input); err == nil {
		return pk, true
	}

	return nostr.ZeroPK, false
}
