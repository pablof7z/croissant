package groups

import (
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

var (
	logg  = log.New(os.Stderr, "[groups] ", log.LstdFlags)
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
		for updated, err := range s.SyncGroupMetadataEvents(affectedGroup) {
			if err != nil {
				logg.Printf("failed to handle group event: %v", err)
			} else {
				s.broadcast(updated)
			}
		}
	}
}

func (s *GroupsState) getGroup(id string) (*Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, ok := s.groups[id]
	return group, ok
}

func (s *GroupsState) setGroup(id string, group *Group) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[id] = group
}

func (s *GroupsState) deleteGroup(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groups, id)
}

func (s *GroupsState) rangeGroups(fn func(string, *Group) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, group := range s.groups {
		if !fn(id, group) {
			return
		}
	}
}

type GroupInfo struct {
	ID      string
	Name    string
	Owner   nostr.PubKey
	Private bool
}

func (s *GroupsState) GetAllGroups() []GroupInfo {
	var result []GroupInfo
	s.rangeGroups(func(id string, group *Group) bool {
		group.mu.RLock()
		defer group.mu.RUnlock()

		// Get the owner (admin role members)
		var owner nostr.PubKey
		for pk, roles := range group.Members {
			for _, role := range roles {
				if role.Name == primaryRoleName {
					owner = pk
					break
				}
			}
			if owner != nostr.ZeroPK {
				break
			}
		}

		result = append(result, GroupInfo{
			ID:      id,
			Name:    group.Name,
			Owner:   owner,
			Private: group.Private,
		})
		return true
	})
	return result
}
