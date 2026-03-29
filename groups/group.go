package groups

import (
	"fmt"
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
				logg.Printf("invalid moderation action: %v", err)
				continue
			}
			act.Apply(&group.Group)
		}

		i := 49
		for evt := range s.DB.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{groupId}}}, 50) {
			group.last50[i] = evt.ID
			i--
		}

		s.setGroup(group.Address.ID, group)
	}

	s.rangeGroups(func(_ string, group *Group) bool {
		for updated, err := range s.SyncGroupMetadataEvents(group) {
			if err != nil {
				return false
			}
			s.broadcast(updated)
		}
		return true
	})

	return nil
}

func (s *GroupsState) GetGroupFromEvent(event nostr.Event) *Group {
	if gid, ok := getGroupIDFromEvent(event); ok {
		group, _ := s.getGroup(gid)
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

func (s *GroupsState) SyncGroupMetadataEvents(group *Group) iter.Seq2[nostr.Event, error] {
	now := nostr.Now()

	return func(yield func(nostr.Event, error) bool) {
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

			if err := event.Sign(s.secretKey); err != nil {
				if !yield(nostr.Event{}, fmt.Errorf("failed to sign group metadata event %d: %w", event.Kind, err)) {
					return
				}
			}

			if err := s.DB.ReplaceEvent(event); err != nil {
				if !yield(nostr.Event{}, fmt.Errorf("failed to save group metadata event %d: %w", event.Kind, err)) {
					return
				}
			}
			if event.CreatedAt > now-180 {
				if !yield(event, nil) {
					return
				}
			}
		}
	}
}
