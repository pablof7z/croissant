package main

import (
	"context"
	"testing"
	"time"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/khatru"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

// TestSyncGroupMetadataEventsReleasesLockBeforeYield reproduces the production
// deadlock observed on nip29.f7z.io (croissant wedged: every group write hung
// with `context deadline exceeded`, CPU 0%, cleared only by a restart).
//
// Live goroutine dump showed the cycle:
//
//	handleEventSaved
//	  → SyncGroupMetadataEvents  holds group.mu.RLock()  (group.go)
//	    → yield → BroadcastEvent → shouldPreventBroadcast → hideEventFromReader
//	      → group.mu.RLock()  AGAIN (re-entrant read)
//
// while a concurrent ProcessEvent for the SAME group waited on group.mu.Lock().
// Go's RWMutex gives a queued writer priority, so the re-entrant second RLock
// can never proceed while the write lock is pending → permanent deadlock, and
// every subsequent reader (rejectEvent, REQ queries) piles up behind it.
//
// The invariant that prevents the whole class of failure: SyncGroupMetadataEvents
// must NOT hold group.mu while the caller processes (broadcasts) a yielded event.
// This test asserts a concurrent writer can take the write lock during the yield.
// With the buggy code the RLock is held across the whole iteration, so the
// writer blocks forever and the test times out.
func TestSyncGroupMetadataEventsReleasesLockBeforeYield(t *testing.T) {
	prevState, prevStore := State, store
	defer func() { State, store = prevState, prevStore }()

	db := &slicestore.SliceStore{}
	db.Init()
	store = db

	sk := nostr.Generate()
	pk := sk.Public()
	member2 := nostr.Generate().Public()

	global.R = khatru.NewRelay()

	State = &GroupsState{
		Groups:        xsync.NewMapOf[string, *Group](),
		AllMembers:    xsync.NewMapOf[nostr.PubKey, int](),
		deletedGroups: xsync.NewMapOf[string, *DeletedGroup](),
		DB:            db,
		secretKey:     sk,
	}

	ctx := context.Background()
	const gid = "groupX"

	// Create the group and persist its initial rosters (create + first member).
	for _, evt := range []nostr.Event{
		{PubKey: pk, CreatedAt: nostr.Now(), Kind: nostr.KindSimpleGroupCreateGroup, Tags: nostr.Tags{{"h", gid}}},
		{PubKey: pk, CreatedAt: nostr.Now(), Kind: nostr.KindSimpleGroupPutUser, Tags: nostr.Tags{{"h", gid}, {"p", pk.Hex()}}},
	} {
		require.NoError(t, evt.Sign(sk))
		handleEventSaved(ctx, evt)
	}

	// Apply an in-memory-only change (add a second member) so the in-memory roster
	// now differs from what SyncGroupMetadataEvents last persisted — guaranteeing
	// the next call yields at least one changed metadata event to iterate over.
	addMember2 := nostr.Event{
		PubKey: pk, CreatedAt: nostr.Now(), Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", gid}, {"p", member2.Hex()}},
	}
	require.NoError(t, addMember2.Sign(sk))
	State.ProcessEvent(ctx, addMember2)

	group, ok := State.Groups.Load(gid)
	require.True(t, ok)

	yielded := 0
	for range State.SyncGroupMetadataEvents(group) {
		yielded++

		// This mirrors what BroadcastEvent does with each yielded event: it (via
		// hideEventFromReader) re-acquires group.mu, and ProcessEvent may be
		// racing for the write lock. Assert the write lock is obtainable while we
		// hold the yielded event — i.e. Sync is not still holding group.mu.
		got := make(chan struct{})
		go func() {
			group.mu.Lock()
			group.mu.Unlock()
			close(got)
		}()
		select {
		case <-got:
		case <-time.After(3 * time.Second):
			t.Fatal("deadlock: SyncGroupMetadataEvents holds group.mu during yield; " +
				"BroadcastEvent → hideEventFromReader re-enters RLock and deadlocks with a pending writer")
		}
	}

	require.Positive(t, yielded, "expected SyncGroupMetadataEvents to yield at least one changed event")
}
