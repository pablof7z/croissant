package main

import (
	"context"
	"time"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	lru "github.com/hashicorp/golang-lru/v2"
)

var (
	freeTransitPresenceCache *lru.Cache[nostr.PubKey, bool]
	giftWrapPresenceCache    *lru.Cache[nostr.PubKey, bool]
)

func init() {
	{
		cache, err := lru.New[nostr.PubKey, bool](2048)
		if err != nil {
			panic(err)
		}
		freeTransitPresenceCache = cache
	}

	{
		cache, err := lru.New[nostr.PubKey, bool](2048)
		if err != nil {
			panic(err)
		}
		giftWrapPresenceCache = cache
	}
}

type CheckType int

const (
	CheckTypeFreeTransit CheckType = iota
	CheckTypeGroupCreate
	CheckTypeGiftWrap
)

//go:inline
func hasPresence(ctx context.Context, pubkey nostr.PubKey, checkType CheckType) bool {
	var relays []string
	var cache *lru.Cache[nostr.PubKey, bool]
	switch checkType {
	case CheckTypeFreeTransit:
		relays = global.S.Groups.FreeTransitPresenceRelays
		if v, ok := freeTransitPresenceCache.Get(pubkey); ok {
			return v
		}
		cache = freeTransitPresenceCache
	case CheckTypeGiftWrap:
		relays = global.S.GiftWraps.SenderPresenceRelays
		if v, ok := giftWrapPresenceCache.Get(pubkey); ok {
			return v
		}
		cache = giftWrapPresenceCache
	case CheckTypeGroupCreate:
		relays = global.S.Groups.CreateGroupPresenceRelays
	}

	if len(relays) == 0 {
		return true
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	filter := nostr.Filter{
		Kinds:   []nostr.Kind{nostr.KindProfileMetadata},
		Authors: []nostr.PubKey{pubkey},
		Limit:   1,
	}

	result := pool.QuerySingle(ctx, relays, filter, nostr.SubscriptionOptions{
		Label: "croissant/presence-check",
	})

	present := result != nil

	if cache != nil {
		cache.Add(pubkey, present)
	}

	return present
}
