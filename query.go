package main

import (
	"context"
	"iter"
	"math"
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
		return func(yield func(nostr.Event) bool) {}
	} else {
		// normal group query
		maxLimit := 1500
		if khatru.IsNegentropySession(ctx) {
			maxLimit = math.MaxInt // no limit: negentropy needs the full set to build the sync vector
		}
		return func(yield func(nostr.Event) bool) {
			for evt := range store.QueryEvents(filter, maxLimit) {
				if hideEventFromReader(filter, evt, authed) {
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
	if ws == nil {
		return true
	}
	return hideEventFromReader(filter, event, ws.AuthedPublicKeys)
}

//go:inline
func hideEventFromReader(filter nostr.Filter, evt nostr.Event, authed []nostr.PubKey) bool {
	// kind:0 events are relay-level (no group), always visible
	if evt.Kind == nostr.KindProfileMetadata {
		return false
	}

	group := State.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	group.mu.RLock()
	hidden := group.Hidden
	private := group.Private
	group.mu.RUnlock()

	if hidden {
		// 'hidden' works only by hiding the group from abrangent queries like listing all groups in a relay etc
		if requestedGroupIds(filter) == nil && filter.IDs == nil {
			return true
		}

		// if specific groups were requested then the 'hidden' field has no effect as the reader
		// already knows about the existence of the group
		// <pass>
	}

	if private {
		// 'private' works by hiding group contents (and member lists etc), but not group metadata
		// group metadata is still public -- UNLESS the group is also marked as hidden, that's a special case
		if evt.Kind == nostr.KindSimpleGroupMetadata {
			if hidden {
				// still allow reading for members only
				if group.AnyOfTheseIsAMember(authed) {
					return false
				}

				return true
			} else {
				// metadata is allowed
				// <pass>
			}
		} else {
			// allow reading for members only
			if group.AnyOfTheseIsAMember(authed) {
				return false
			}
		}
	}

	return false
}

//go:inline
func requestedGroupIds(filter nostr.Filter) []string {
	groupIds, _ := filter.Tags["h"]
	if len(groupIds) == 0 {
		groupIds, _ = filter.Tags["d"]
	}
	return groupIds
}
