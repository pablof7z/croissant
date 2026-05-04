package main

import (
	"iter"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/bleve"
	"fiatjaf.com/nostr/nip29"
	"github.com/pemistahl/lingua-go"
	"github.com/rs/zerolog/log"
)

const (
	primaryRoleName   = "admin"
	secondaryRoleName = "moderator"
)

type Group struct {
	nip29.Group
	mu sync.RWMutex

	last50      []nostr.ID
	last50index atomic.Int32

	searchLanguage string
	searchIndex    *bleve.BleveBackend
}

type DeletedGroup struct {
	ID        string
	DeletedAt nostr.Timestamp
	DeletedBy nostr.PubKey
}

func (s *GroupsState) NewGroup(id string) *Group {
	return &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: s.relayURL,
			},
			Roles: []*nip29.Role{
				{Name: primaryRoleName},
				{Name: secondaryRoleName},
			},
			Members:     make(map[nostr.PubKey][]*nip29.Role),
			InviteCodes: make([]string, 0),
		},
		last50: make([]nostr.ID, 50),
	}
}

func (s *GroupsState) loadGroupsFromDB() error {
nextgroup:
	for evt := range s.DB.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindSimpleGroupCreateGroup}}, 500_000) {
		groupId, ok := getGroupIDFromEvent(evt)
		if !ok {
			continue
		}

		group := s.NewGroup(groupId)
		events := make([]nostr.Event, 0, 5000)
		for event := range s.DB.QueryEvents(nostr.Filter{
			Kinds: nip29.ModerationEventKinds,
			Tags:  nostr.TagMap{"h": []string{groupId}},
		}, 50_000) {
			if event.Kind == nostr.KindSimpleGroupDeleteGroup {
				s.deletedGroups.Store(groupId, &DeletedGroup{
					ID:        groupId,
					DeletedAt: event.CreatedAt,
					DeletedBy: event.PubKey,
				})
				continue nextgroup
			}
			events = append(events, event)
		}

		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, err := nip29.PrepareModerationAction(evt)
			if err != nil {
				L.Warn().Err(err).Msg("invalid moderation action")
				continue
			}
			act.Apply(&group.Group)

			// update AllMembers counts
			switch act := act.(type) {
			case nip29.PutUser:
				for _, target := range act.Targets {
					s.AllMembers.Compute(target.PubKey, func(count int, exists bool) (newV int, delete bool) {
						return count + 1, false
					})
				}
			case nip29.RemoveUser:
				for _, targetPubKey := range act.Targets {
					s.AllMembers.Compute(targetPubKey, func(count int, exists bool) (newV int, delete bool) {
						return count - 1, count <= 1
					})
				}
			}
		}

		i := 49
		for evt := range s.DB.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{groupId}}}, 50) {
			group.last50[i] = evt.ID
			i--
		}

		s.Groups.Store(group.Address.ID, group)
		s.deletedGroups.Delete(group.Address.ID)
	}

	for _, group := range s.Groups.Range {
		for updated := range s.SyncGroupMetadataEvents(group) {
			global.R.BroadcastEvent(updated)
		}
	}

	return nil
}

func (s *GroupsState) LoadDeletedGroupState(groupID string) (*Group, *DeletedGroup, error) {
	deletedGroup, ok := s.deletedGroups.Load(groupID)
	if !ok {
		return nil, nil, nil
	}

	group := s.NewGroup(groupID)
	events := make([]nostr.Event, 0, 128)
	for event := range s.DB.QueryEvents(nostr.Filter{
		Kinds: nip29.ModerationEventKinds,
		Tags:  nostr.TagMap{"h": []string{groupID}},
	}, 50_000) {
		if event.Kind == nostr.KindSimpleGroupDeleteGroup || event.CreatedAt > deletedGroup.DeletedAt {
			continue
		}
		events = append(events, event)
	}

	for i := len(events) - 1; i >= 0; i-- {
		act, err := nip29.PrepareModerationAction(events[i])
		if err != nil {
			L.Warn().Err(err).Str("group", groupID).Msg("invalid moderation action")
			continue
		}
		act.Apply(&group.Group)
	}

	return group, deletedGroup, nil
}

func (s *GroupsState) GetGroupFromEvent(event nostr.Event) *Group {
	if gid, ok := getGroupIDFromEvent(event); ok {
		group, _ := s.Groups.Load(gid)
		return group
	}
	return nil
}

func getGroupIDFromEvent(event nostr.Event) (string, bool) {
	if nip29.MetadataEventKinds.Includes(event.Kind) {
		gtag := event.Tags.Find("d")
		if gtag != nil {
			return gtag[1], true
		}
	} else {
		gtag := event.Tags.Find("h")
		if gtag != nil {
			return gtag[1], true
		}
	}
	return "", false
}

func (s *GroupsState) SyncGroupMetadataEvents(group *Group) iter.Seq[nostr.Event] {
	now := nostr.Now()

	return func(yield func(nostr.Event) bool) {
		group.mu.RLock()
		defer group.mu.RUnlock()

		for _, updated := range [4]nostr.Event{
			group.ToMetadataEvent(),
			group.ToAdminsEvent(),
			group.ToMembersEvent(),
			group.ToRolesEvent(),
		} {
			// first check if we really have to update this
			var current nostr.Event
			for existing := range s.DB.QueryEvents(nostr.Filter{
				Kinds:   []nostr.Kind{updated.Kind},
				Authors: []nostr.PubKey{s.secretKey.Public()},
				Tags:    nostr.TagMap{"d": []string{group.Address.ID}},
			}, 1) {
				current = existing
			}
			if current.Tags.Eq(updated.Tags) {
				continue
			}

			updated.Sign(s.secretKey)

			if deleted, err := s.DB.ReplaceEvent(updated); err != nil {
				L.Error().Int("kind", int(updated.Kind.Num())).Err(err).Msg("failed to save group metadata event")
			} else {
				if updated.Kind == nostr.KindSimpleGroupMetadata {
					for _, del := range deleted {
						if err := GlobalSearchIndex.DeleteEvent(del.ID); err != nil {
							L.Error().Int("kind", int(updated.Kind.Num())).Err(err).Msg("failed to deindex group metadata event")
						}
					}
				}

				if err := GlobalSearchIndex.SaveEvent(updated); err != nil {
					L.Error().Int("kind", int(updated.Kind.Num())).Err(err).Msg("failed to index group metadata event")
				}
			}
			if updated.CreatedAt > now-180 {
				if !yield(updated) {
					return
				}
			}
		}
	}
}

func (g *Group) AnyOfTheseIsAMember(pubkeys []nostr.PubKey) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, pk := range pubkeys {
		if _, isMember := g.Members[pk]; isMember {
			return true
		}
	}
	return false
}

func (g *Group) IndexEvent(event nostr.Event) error {
	if !slices.Contains(indexableKinds, event.Kind) {
		return nil
	}

	index, err := g.ensureIndex()
	if err != nil {
		return err
	}
	if index == nil {
		return nil
	}

	return index.SaveEvent(event)
}

func (g *Group) SearchEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	// ensure we're only filtering for supported kinds (or nothing)
	if filter.Kinds != nil {
		for i := 0; i < len(filter.Kinds); i++ {
			if !slices.Contains(indexableKinds, filter.Kinds[i]) {
				// swap-delete
				filter.Kinds[i] = filter.Kinds[len(filter.Kinds)-1]
				filter.Kinds = filter.Kinds[0 : len(filter.Kinds)-1]
				i--
			}
		}
	}

	if len(filter.Kinds) == 0 {
		return func(yield func(nostr.Event) bool) {}
	}

	// ensure we have an index
	index, _ := g.ensureIndex()
	if index != nil {
		return index.QueryEvents(filter, maxLimit)
	}

	return func(yield func(nostr.Event) bool) {}
}

func (g *Group) ensureIndex() (*bleve.BleveBackend, error) {
	// we already have the index on memory?
	if g.searchIndex != nil {
		return g.searchIndex, nil
	}

	// load an index we already have on disk
	indexPath := filepath.Join(global.E.DataPath, "search", g.Address.ID)
	langCode, ok, err := readLanguage(indexPath)
	if err != nil {
		return nil, err
	}

	if ok {
		isoCode := lingua.GetIsoCode639_1FromValue(langCode)
		lang := lingua.GetLanguageFromIsoCode639_1(isoCode)

		g.searchIndex = &bleve.BleveBackend{
			Path:           indexPath,
			Languages:      []lingua.Language{lang},
			IndexableKinds: indexableKinds,
			RawEventStore:  store,
		}
		if err := g.searchIndex.Init(); err != nil {
			return nil, err
		}

		return g.searchIndex, nil
	}

	// try to create the index
	count, err := store.CountEvents(nostr.Filter{
		Kinds: indexableKinds,
		Tags:  nostr.TagMap{"h": []string{g.Address.ID}},
	})
	if err != nil {
		return nil, err
	}
	if count <= 10 {
		return nil, nil
	}

	events, combinedContent := g.collectGroupContent(count)
	lang := detectLanguage(combinedContent)

	g.searchIndex = &bleve.BleveBackend{
		Path:           indexPath,
		Languages:      []lingua.Language{lang},
		IndexableKinds: indexableKinds,
		RawEventStore:  store,
	}
	if err := g.searchIndex.Init(); err != nil {
		return nil, err
	}
	if err := writeLanguage(indexPath, langCode); err != nil {
		g.searchIndex.Close()
		return nil, err
	}

	for _, evt := range events {
		if err := g.searchIndex.SaveEvent(evt); err != nil {
			log.Warn().Err(err).Str("group", g.Address.ID).Str("event", evt.ID.Hex()).Msg("failed to index event")
		}
	}

	return g.searchIndex, nil
}

func (g *Group) collectGroupContent(count uint32) ([]nostr.Event, string) {
	maxInt := int(^uint(0) >> 1)
	maxLimit := int(count)
	if maxLimit <= 0 || maxLimit > maxInt {
		maxLimit = maxInt
	}

	events := make([]nostr.Event, 0, maxLimit)
	var combined strings.Builder

	for evt := range store.QueryEvents(nostr.Filter{
		Kinds: indexableKinds,
		Tags:  nostr.TagMap{"h": []string{g.Address.ID}},
	}, maxLimit) {
		events = append(events, evt)
		if evt.Content == "" {
			continue
		}
		if combined.Len() > 0 {
			combined.WriteByte('\n')
		}
		combined.WriteString(evt.Content)
	}

	return events, combined.String()
}
