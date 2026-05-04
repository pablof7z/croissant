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

func TestHideEventFromReader(t *testing.T) {
	prevState := State
	defer func() { State = prevState }()

	State = &GroupsState{Groups: xsync.NewMapOf[string, *Group]()}

	member := nostr.PubKey{1}
	nonMember := nostr.PubKey{2}

	group := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "secret"},
		Members: map[nostr.PubKey][]*nip29.Role{},
	}}
	group.Hidden = true
	group.Private = true
	group.Members[member] = nil
	State.Groups.Store(group.Address.ID, group)

	metadata := nostr.Event{
		Kind: nostr.KindSimpleGroupMetadata,
		Tags: nostr.Tags{{"d", group.Address.ID}},
	}

	requested := nostr.Filter{Tags: nostr.TagMap{"d": {group.Address.ID}}}

	require.True(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{nonMember}),
		"expected metadata to be hidden from non-member when group is private and hidden",
	)

	require.False(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{member}),
		"expected metadata to be visible to member when group is private and hidden",
	)

	group.Hidden = false
	require.False(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{nonMember}),
		"expected private-only group metadata to stay visible to non-member when explicitly requested",
	)
}

func TestQueryConditions(t *testing.T) {
	db := &slicestore.SliceStore{}
	db.Init()
	store = db

	tmpDir, _ := os.MkdirTemp("", "croissant-test")
	defer os.RemoveAll(tmpDir)

	GlobalSearchIndex = &bleve.BleveBackend{
		Path: tmpDir,
	}

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

	// create groups
	for _, id := range []string{"groupA", "groupB"} {
		{
			evt := nostr.Event{
				PubKey:    pk,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindSimpleGroupCreateGroup,
				Tags:      nostr.Tags{{"h", id}},
			}
			require.NoError(t, evt.Sign(sk))
			handleEventSaved(ctx, evt)
		}

		{
			evt := nostr.Event{
				PubKey:    pk,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindSimpleGroupPutUser,
				Tags:      nostr.Tags{{"h", id}, {"p", pk.Hex()}},
			}
			require.NoError(t, evt.Sign(sk))
			handleEventSaved(ctx, evt)
		}
	}

	// make group B hidden
	editHidden := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      nostr.Kind(9002),
		Tags:      nostr.Tags{{"h", "groupB"}, {"hidden"}},
	}
	require.NoError(t, editHidden.Sign(sk))
	handleEventSaved(ctx, editHidden)

	{
		// when all groups are requested the hidden group doesn't come
		var broadResults []nostr.Event
		for evt := range query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		}) {
			broadResults = append(broadResults, evt)
		}
		require.Len(t, broadResults, 1)
		require.Equal(t, "groupA", broadResults[0].Tags.GetD())
	}

	{
		// the public group shows up when queried by id
		var requestedResults []nostr.Event
		for evt := range query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupA"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 1)
		require.Equal(t, "groupA", requestedResults[0].Tags.GetD())
	}

	{
		// the hidden group shows up when queried by id
		var requestedResults []nostr.Event
		for evt := range query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 1)
		require.Equal(t, "groupB", requestedResults[0].Tags.GetD())
	}

	// make group B not only hidden but also private
	editPrivate := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      nostr.Kind(9002),
		Tags:      nostr.Tags{{"h", "groupB"}, {"hidden"}, {"private"}},
	}
	require.NoError(t, editPrivate.Sign(sk))
	handleEventSaved(ctx, editPrivate)

	{
		// the hidden group doesn't show up when queried by id anymore
		var requestedResults []nostr.Event
		for evt := range query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 0)
	}

	{
		// but it should show up when queried by id by an authed member
		var broadResults []nostr.Event
		for evt := range query(khatru.ForceSetAuthed(ctx, pk), nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			broadResults = append(broadResults, evt)
		}
		require.Len(t, broadResults, 1)
		require.Equal(t, "groupB", broadResults[0].Tags.GetD())
	}
}
