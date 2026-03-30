package groups

import (
	"iter"
	"sync"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
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
	for evt := range s.DB.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindSimpleGroupCreateGroup}}, 5000) {
		groupId, ok := getGroupIDFromEvent(evt)
		if !ok {
			continue
		}

		group := s.NewGroup(groupId)
		events := make([]nostr.Event, 0, 5000)
		for event := range s.DB.QueryEvents(nostr.Filter{
			Kinds: nip29.ModerationEventKinds,
			Tags:  nostr.TagMap{"h": []string{groupId}},
		}, 50000) {
			if event.Kind == nostr.KindSimpleGroupDeleteGroup {
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
		}

		i := 49
		for evt := range s.DB.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{groupId}}}, 50) {
			group.last50[i] = evt.ID
			i--
		}

		s.SetGroup(group.Address.ID, group)
	}

	for group := range s.ListGroups() {
		for updated := range s.SyncGroupMetadataEvents(group) {
			s.broadcast(updated)
		}
	}

	return nil
}

func (s *GroupsState) GetGroupFromEvent(event nostr.Event) *Group {
	if gid, ok := getGroupIDFromEvent(event); ok {
		group, _ := s.GetGroup(gid)
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

		for _, event := range [4]nostr.Event{
			group.ToMetadataEvent(),
			group.ToAdminsEvent(),
			group.ToMembersEvent(),
			group.ToRolesEvent(),
		} {
			if group.Private && event.Kind == nostr.KindSimpleGroupMembers {
				continue
			}

			event.Sign(s.secretKey)

			if err := s.DB.ReplaceEvent(event); err != nil {
				L.Error().Int("kind", int(event.Kind.Num())).Err(err).Msg("failed to save group metadata event")
			}
			if event.CreatedAt > now-180 {
				if !yield(event) {
					return
				}
			}
		}
	}
}
