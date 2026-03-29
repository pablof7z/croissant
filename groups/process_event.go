package groups

import (
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

func (s *GroupsState) ProcessEvent(event nostr.Event) (groupsAffected []*Group) {
	if action, err := nip29.PrepareModerationAction(event); err == nil {
		var group *Group
		if event.Kind == nostr.KindSimpleGroupCreateGroup {
			groupId, ok := getGroupIDFromEvent(event)
			if !ok {
				logg.Printf("event without a group id")
				return nil
			}

			group = s.NewGroup(groupId)
			s.setGroup(groupId, group)
			groupsAffected = nostr.AppendUnique(groupsAffected, group)
		} else {
			group = s.GetGroupFromEvent(event)
			if group == nil {
				return nil
			}
			groupsAffected = nostr.AppendUnique(groupsAffected, group)
		}

		group.mu.Lock()
		action.Apply(&group.Group)
		group.mu.Unlock()

		if event.Kind == nostr.KindSimpleGroupDeleteEvent {
			for tag := range event.Tags.FindAll("e") {
				id, err := nostr.IDFromHex(tag[1])
				if err != nil {
					continue
				}
				if err := s.DB.DeleteEvent(id); err != nil {
					logg.Printf("failed to delete event: %v", err)
				} else {
					idx := s.deletedCacheIndex.Add(1) % uint32(len(s.deletedCache))
					s.deletedCache[idx] = id
				}
			}
		} else if event.Kind == nostr.KindSimpleGroupDeleteGroup {
			s.deleteGroup(group.Address.ID)
		}
	}

	group := s.GetGroupFromEvent(event)
	if group == nil {
		return groupsAffected
	}

	groupsAffected = nostr.AppendUnique(groupsAffected, group)

	lastIndex := group.last50index.Add(1) - 1
	group.last50[lastIndex%50] = event.ID

	return groupsAffected
}
