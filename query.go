package main

import (
	"context"
	"iter"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	authed := khatru.GetAllAuthed(ctx)

	if slices.Contains(filter.Kinds, 1059) {
		// gift-wrap query
		// if we have kind:1059 that means we necessarily also have at least one "p" tag
		// and that is already authorized to read (see rejectRequest)
		return store.QueryEvents(filter, 500)
	} else if filter.Search != "" {
		// search for groups
		if len(filter.Kinds) == 1 && filter.Kinds[0] == nostr.KindSimpleGroupMetadata {
			return func(yield func(nostr.Event) bool) {
				for evt := range GlobalSearchIndex.QueryEvents(filter, 40) {
					if group := State.GetGroupFromEvent(evt); group != nil {
						if !group.Hidden || group.AnyOfTheseIsAMember(authed) {
							if !yield(evt) {
								return
							}
						}
					}
				}
			}
		}

		// search inside one or more specific groups
		// (already gated to require between 1 and 5 group ids when searching)
		groupIDs, _ := filter.Tags["h"]
		return func(yield func(nostr.Event) bool) {
			for _, groupId := range groupIDs {
				if group, ok := State.Groups.Load(groupId); ok {
					if !group.Private || group.AnyOfTheseIsAMember(authed) {
						for evt := range group.SearchEvents(filter, 40) {
							if !yield(evt) {
								return
							}
						}
					}
				}
			}
		}
	} else {
		// normal group query
		return func(yield func(nostr.Event) bool) {
			for evt := range store.QueryEvents(filter, 1500) {
				if hideEventFromReader(evt, authed) {
					continue
				}

				if !yield(evt) {
					return
				}
			}
		}
	}
}

//go:inline
func shouldPreventBroadcast(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
	return hideEventFromReader(event, ws.AuthedPublicKeys)
}

//go:inline
func hideEventFromReader(evt nostr.Event, authed []nostr.PubKey) bool {
	group := State.GetGroupFromEvent(evt)
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
