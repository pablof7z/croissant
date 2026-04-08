package main

import (
	"net/http"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"

	"fiatjaf.com/croissant/global"
)

func wipeGroupHandler(w http.ResponseWriter, r *http.Request) {
	loggedPubKey, ok := global.GetLoggedUser(r)
	if !ok || loggedPubKey != global.S.OwnerPubKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	groupID := r.PathValue("id")
	group, ok := State.Groups.Load(groupID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	deleted := make(map[nostr.ID]struct{})
	deleteEvent := func(evt nostr.Event) {
		if _, seen := deleted[evt.ID]; seen {
			return
		}
		deleted[evt.ID] = struct{}{}
		if err := store.DeleteEvent(evt.ID); err != nil {
			L.Warn().Err(err).Str("event", evt.ID.Hex()).Msg("failed to delete event")
		}
	}

	for evt := range store.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{groupID}}}, 10_000_000) {
		deleteEvent(evt)
	}
	for evt := range store.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"d": []string{groupID}}}, 10_000_000) {
		deleteEvent(evt)
	}

	group.mu.RLock()
	for member := range group.Members {
		State.AllMembers.Compute(member, func(count int, exists bool) (newV int, delete bool) {
			if !exists {
				return 0, true
			}
			newV = count - 1
			return newV, newV <= 0
		})
	}
	group.mu.RUnlock()

	if group.searchIndex != nil {
		group.searchIndex.Close()
	}
	indexPath := filepath.Join(global.E.DataPath, "search", groupID)
	if err := os.RemoveAll(indexPath); err != nil {
		L.Warn().Err(err).Str("path", indexPath).Msg("failed to remove group search index")
	}

	State.Groups.Delete(groupID)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
