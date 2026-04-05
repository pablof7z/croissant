package main

import (
	"context"
	"time"

	"fiatjaf.com/nostr"
	"github.com/hashicorp/golang-lru/v2"
)

const freeTransitPresenceCacheSize = 2048

var freeTransitPresenceCache *lru.Cache[nostr.PubKey, bool]

func init() {
	cache, err := lru.New[nostr.PubKey, bool](freeTransitPresenceCacheSize)
	if err != nil {
		panic(err)
	}
	freeTransitPresenceCache = cache
}

func hasPresence(ctx context.Context, relays []string, pubkey nostr.PubKey, isFreeTransit bool) bool {
	if len(relays) == 0 {
		// if nothing is specified anyone is allowed
		return true
	}

	if isFreeTransit {
		if value, ok := freeTransitPresenceCache.Get(pubkey); ok {
			return value
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	filter := nostr.Filter{
		Kinds:   []nostr.Kind{nostr.KindProfileMetadata},
		Authors: []nostr.PubKey{pubkey},
		Limit:   1,
	}

	result := pool.QuerySingle(ctx, relays, filter, nostr.SubscriptionOptions{})
	present := result != nil
	if isFreeTransit {
		freeTransitPresenceCache.Add(pubkey, present)
	}
	return present
}
