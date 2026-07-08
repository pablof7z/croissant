package main

import (
	"context"
	"testing"
	"time"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

// TestProcessEventReleasesGroupLockOnApplyPanic guards the group write lock
// against being leaked when action.Apply panics.
//
// ProcessEvent took group.mu.Lock() and called action.Apply WITHOUT a deferred
// Unlock. A panic inside Apply (e.g. a group whose Members map is nil, so
// PutUser.Apply's `group.Members[pk] = roles` panics with "assignment to entry
// in nil map") therefore left the group permanently write-locked — the same
// class of wedge as the SyncGroupMetadataEvents deadlock, just triggered by a
// panic instead of re-entrancy. Every subsequent reader/writer on that group
// then hangs forever until the process is restarted.
//
// With the deferred unlock the lock is released as the panic unwinds, so the
// group stays usable.
func TestProcessEventReleasesGroupLockOnApplyPanic(t *testing.T) {
	prevState, prevStore := State, store
	defer func() { State, store = prevState, prevStore }()

	db := &slicestore.SliceStore{}
	db.Init()
	store = db

	sk := nostr.Generate()
	pk := sk.Public()

	global.R = khatru.NewRelay()

	State = &GroupsState{
		Groups:        xsync.NewMapOf[string, *Group](),
		AllMembers:    xsync.NewMapOf[nostr.PubKey, int](),
		deletedGroups: xsync.NewMapOf[string, *DeletedGroup](),
		DB:            db,
		secretKey:     sk,
	}

	const gid = "brokengroup"
	// A malformed group with a NIL Members map. PutUser.Apply writes into it and
	// panics — while ProcessEvent holds group.mu.
	group := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: gid},
		Roles:   []*nip29.Role{{Name: primaryRoleName}, {Name: secondaryRoleName}},
		// Members intentionally left nil to force the panic.
	}}
	State.Groups.Store(gid, group)

	put := nostr.Event{
		PubKey: pk, CreatedAt: nostr.Now(), Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", gid}, {"p", pk.Hex()}},
	}
	require.NoError(t, put.Sign(sk))

	// khatru runs OnEventSaved in a way that would recover a panic; emulate that
	// so the test observes the lock state rather than crashing.
	func() {
		defer func() { _ = recover() }()
		State.ProcessEvent(context.Background(), put)
	}()

	got := make(chan struct{})
	go func() {
		group.mu.Lock()
		group.mu.Unlock()
		close(got)
	}()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("group.mu write lock leaked after a panic in action.Apply " +
			"(ProcessEvent must defer group.mu.Unlock())")
	}
}
