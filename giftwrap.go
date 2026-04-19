package main

import (
	"context"
	"encoding/hex"
	"unsafe"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

//go:inline
func authedAreTheSameAsPTagged(authed []nostr.PubKey, pTags []string) bool {
	if len(pTags) != len(authed) {
		return false
	}

	var ax [64]byte
	for _, a := range authed {
		hex.Encode(ax[:], a[:])
		axs := unsafe.String(unsafe.SliceData(ax[:]), 64)
		ok := false
		for _, pTag := range pTags {
			if pTag == axs {
				ok = true
				break
			}
		}
		if ok == false {
			return false
		}
	}

	return true
}

func rejectGiftWrapEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	if !global.S.GiftWraps.Enabled {
		return true, "blocked: gift-wraps are disabled"
	}

	receiverPresent := false
	for ptag := range event.Tags.FindAll("p") {
		if ptag == nil || len(ptag) < 2 {
			return true, "invalid recipient (`p`) tag"
		}
		recipient, err := nostr.PubKeyFromHex(ptag[1])
		if err != nil {
			return true, "invalid recipient pubkey"
		}

		if !receiverPresent && hasPresence(ctx, recipient, CheckTypeGiftWrap) {
			receiverPresent = true
		}
	}

	if len(global.S.GiftWraps.SenderPresenceRelays) == 0 {
		return false, ""
	}

	authed, ok := khatru.GetAuthed(ctx)
	if !ok {
		return true, "auth-required: sender must authenticate"
	}

	if !hasPresence(ctx, authed, CheckTypeGiftWrap) {
		return true, "restricted: sender not in presence relays"
	}

	return false, ""
}
