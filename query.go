package main

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func (s *GroupsState) Query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		authed := khatru.GetAllAuthed(ctx)

		for evt := range s.DB.QueryEvents(filter, 1500) {
			if s.hideEventFromReader(evt, authed) {
				continue
			}

			if !yield(evt) {
				return
			}
		}
	}
}

func (s *GroupsState) ShouldPreventBroadcast(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
	return s.hideEventFromReader(event, ws.AuthedPublicKeys)
}

//go:inline
func (s *GroupsState) hideEventFromReader(evt nostr.Event, authed []nostr.PubKey) bool {
	group := s.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	// filtering by checking if a user is a member of a group (when 'private') is already done by
	// s.RequestAuthWhenNecessary(), so we don't have to do it here
	// assume the requester has access to all these groups
	if !group.Hidden && !group.Private {
		return false
	} else if group.AnyOfTheseIsAMember(authed) {
		return false
	}

	return false
}
