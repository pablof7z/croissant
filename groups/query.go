package groups

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
)

func (s *GroupsState) Query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	_ = ctx
	return func(yield func(nostr.Event) bool) {
		for evt := range s.DB.QueryEvents(filter, 1500) {
			if !yield(evt) {
				return
			}
		}
	}
}

func (s *GroupsState) ShouldPreventBroadcast(evt nostr.Event, filter nostr.Filter) bool {
	_ = evt
	_ = filter
	return false
}
