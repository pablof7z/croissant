package groups

import (
	"fmt"
	"iter"
	"sync"
	"sync/atomic"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

var (
	L     = global.L.With().Str("c", "groups").Logger()
	State *GroupsState
)

type LiveKitSettings struct {
	ServerURL string
	APIKey    string
	APISecret string
}

type Options struct {
	DB        eventstore.Store
	SecretKey nostr.SecretKey
	Broadcast func(nostr.Event) int
	RelayURL  string
	BaseURL   string
	LiveKit   LiveKitSettings
}

type GroupsState struct {
	mu       sync.RWMutex
	groups   map[string]*Group
	DB       eventstore.Store
	relayURL string
	baseURL  string
	livekit  LiveKitSettings

	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Uint32

	secretKey nostr.SecretKey
	broadcast func(nostr.Event) int
}

func Init(opts Options) {
	State = NewGroupsState(opts)
}

func NewGroupsState(opts Options) *GroupsState {
	state := &GroupsState{
		groups:    make(map[string]*Group, 64),
		DB:        opts.DB,
		relayURL:  opts.RelayURL,
		baseURL:   opts.BaseURL,
		livekit:   opts.LiveKit,
		secretKey: opts.SecretKey,
		broadcast: opts.Broadcast,
	}

	if err := state.loadGroupsFromDB(); err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}

func (s *GroupsState) HandleEventSaved(event nostr.Event) {
	for _, affectedGroup := range s.ProcessEvent(event) {
		for updated := range s.SyncGroupMetadataEvents(affectedGroup) {
			s.broadcast(updated)
		}
	}
}

func (s *GroupsState) GetGroup(id string) (*Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, ok := s.groups[id]
	return group, ok
}

func (s *GroupsState) SetGroup(id string, group *Group) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[id] = group
}

func (s *GroupsState) DeleteGroup(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groups, id)
}

func (s *GroupsState) CountGroups() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.groups)
}

func (s *GroupsState) ListGroups() iter.Seq[*Group] {
	return func(yield func(*Group) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, group := range s.groups {
			if !yield(group) {
				return
			}
		}
	}
}
