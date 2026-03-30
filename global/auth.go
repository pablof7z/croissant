package global

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

const nip98CookieName = "nip98"

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

	expectedDomain := S.Domain
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
