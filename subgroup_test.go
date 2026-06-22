package main

import (
	"context"
	"os"
	"testing"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/bleve"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

// TestSubgroupMetadataEmission checks that a child group created with a
// ["parent", <id>] tag re-emits that tag on its kind:39000 metadata event,
// while a plain group emits no parent tag.
func TestSubgroupMetadataEmission(t *testing.T) {
	prevState := State
	defer func() { State = prevState }()

	db := &slicestore.SliceStore{}
	db.Init()
	store = db

	tmpDir, _ := os.MkdirTemp("", "croissant-subgroup-test")
	defer os.RemoveAll(tmpDir)
	GlobalSearchIndex = &bleve.BleveBackend{Path: tmpDir}

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

	ctx := context.Background()

	createGroup := func(id string, extraTags ...nostr.Tag) {
		tags := nostr.Tags{{"h", id}}
		tags = append(tags, extraTags...)
		evt := nostr.Event{
			PubKey:    pk,
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupCreateGroup,
			Tags:      tags,
		}
		require.NoError(t, evt.Sign(sk))
		handleEventSaved(ctx, evt)
	}

	createGroup("parentG")
	createGroup("childG", nostr.Tag{"parent", "parentG"})

	parentTagOf := func(id string) (string, bool) {
		for evt := range query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {id}},
		}) {
			if t := evt.Tags.Find("parent"); t != nil && len(t) >= 2 {
				return t[1], true
			}
			return "", false
		}
		t.Fatalf("no metadata event found for group %q", id)
		return "", false
	}

	parent, ok := parentTagOf("childG")
	require.True(t, ok, "child group metadata should carry a parent tag")
	require.Equal(t, "parentG", parent)

	_, ok = parentTagOf("parentG")
	require.False(t, ok, "a top-level group must not carry a parent tag")
}

// TestSubgroupCreationValidation exercises the create-time rejection rules for
// subgroups: parent must exist, creator must be an admin of the parent, and the
// link must not form a cycle.
func TestSubgroupCreationValidation(t *testing.T) {
	prevState := State
	prevSettings := global.S
	defer func() { State = prevState; global.S = prevSettings }()

	// a relay key distinct from the test keys (otherwise rejectEvent bypasses).
	global.S.RelaySecretKey = nostr.Generate()

	State = &GroupsState{Groups: xsync.NewMapOf[string, *Group]()}

	adminSk := nostr.Generate()
	admin := adminSk.Public()
	strangerSk := nostr.Generate()

	mkGroup := func(id, parent string, members map[nostr.PubKey][]*nip29.Role) *Group {
		g := &Group{Group: nip29.Group{
			Address: nip29.GroupAddress{ID: id},
			Members: members,
		}}
		g.Parent = parent
		State.Groups.Store(id, g)
		return g
	}

	mkGroup("p", "", map[nostr.PubKey][]*nip29.Role{
		admin: {{Name: primaryRoleName}},
	})

	createChild := func(id, parent string, sk nostr.SecretKey) (bool, string) {
		evt := nostr.Event{
			PubKey:    sk.Public(),
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupCreateGroup,
			Tags:      nostr.Tags{{"h", id}, {"parent", parent}},
		}
		require.NoError(t, evt.Sign(sk))
		return rejectEvent(context.Background(), evt)
	}

	t.Run("admin of parent succeeds", func(t *testing.T) {
		reject, msg := createChild("child1", "p", adminSk)
		require.False(t, reject, "expected accept, got: %s", msg)
	})

	t.Run("non-admin is rejected", func(t *testing.T) {
		reject, msg := createChild("child2", "p", strangerSk)
		require.True(t, reject)
		require.Contains(t, msg, "admin of the parent")
	})

	t.Run("missing parent is rejected", func(t *testing.T) {
		reject, msg := createChild("child3", "does-not-exist", adminSk)
		require.True(t, reject)
		require.Contains(t, msg, "doesn't exist")
	})

	t.Run("cycle is rejected", func(t *testing.T) {
		// parent "pc" already points at the about-to-be-created "c" (as could
		// happen via delete+recreate), so creating "c" parent="pc" forms c->pc->c.
		mkGroup("pc", "c", map[nostr.PubKey][]*nip29.Role{
			admin: {{Name: primaryRoleName}},
		})
		reject, msg := createChild("c", "pc", adminSk)
		require.True(t, reject)
		require.Contains(t, msg, "cycle")
	})
}

// TestWouldCreateCycle unit-tests the cycle detector directly.
func TestWouldCreateCycle(t *testing.T) {
	prevState := State
	defer func() { State = prevState }()

	State = &GroupsState{Groups: xsync.NewMapOf[string, *Group]()}

	mk := func(id, parent string) {
		g := &Group{Group: nip29.Group{Address: nip29.GroupAddress{ID: id}}}
		g.Parent = parent
		State.Groups.Store(id, g)
	}

	// chain: a (root) <- b <- c
	mk("a", "")
	mk("b", "a")
	mk("c", "b")

	// a brand-new group "d" under any existing node never cycles
	require.False(t, State.wouldCreateCycle("d", "c"))
	require.False(t, State.wouldCreateCycle("d", "a"))

	// re-parenting "a" under its descendant "c" would form a->...->a
	require.True(t, State.wouldCreateCycle("a", "c"))
	// self-parenting
	require.True(t, State.wouldCreateCycle("a", "a"))
	// dangling parent terminates the walk: no cycle
	require.False(t, State.wouldCreateCycle("x", "nonexistent"))
}
