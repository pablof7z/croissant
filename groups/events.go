package groups

import (
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

func IsGroupEvent(event nostr.Event) bool {
	if event.Tags.Find("h") != nil {
		return true
	}
	if nip29.MetadataEventKinds.Includes(event.Kind) {
		return true
	}
	return false
}
