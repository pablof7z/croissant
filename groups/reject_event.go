package groups

import (
	"context"
	"fmt"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

func (s *GroupsState) RejectEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	_ = ctx

	if nip29.ModerationEventKinds.Includes(event.Kind) && event.CreatedAt < nostr.Now()-60 {
		return true, "moderation action is too old (older than 1 minute ago)"
	}

	if nip29.MetadataEventKinds.Includes(event.Kind) {
		return true, "restricted: group metadata events are generated internally"
	}

	groupId, ok := getGroupIDFromEvent(event)
	if !ok {
		return true, "missing group (`h`) tag"
	}

	group := s.GetGroupFromEvent(event)

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		if group != nil {
			return true, "duplicate: group already exists"
		}
		return false, ""
	}

	if !ok || group == nil {
		return true, "group '" + groupId + "' doesn't exist"
	}

	if slices.Contains(s.deletedCache[:], event.ID) {
		return true, "blocked: this was deleted"
	}

	if group.SupportedKinds != nil {
		if len(group.SupportedKinds) == 0 && group.LiveKit {
			return true, "blocked: this is a live audio/video group only"
		}
		if !slices.Contains(group.SupportedKinds, event.Kind) {
			return true, "blocked: kind not supported by this group"
		}
	}

	previous := event.Tags.Find("previous")
	if previous != nil {
		for _, idFirstChars := range previous[1:] {
			if len(idFirstChars) > 64 {
				return true, fmt.Sprintf("error: invalid value '%s' in previous tag", idFirstChars)
			}
			found := false
			for _, id := range group.last50 {
				if id == nostr.ZeroID {
					continue
				}
				if id.Hex()[0:len(idFirstChars)] == idFirstChars {
					found = true
					break
				}
			}
			if !found {
				return true, fmt.Sprintf("previous id '%s' wasn't found in this group", idFirstChars)
			}
		}
	}

	return false, ""
}
