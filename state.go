package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"github.com/puzpuzpuz/xsync/v3"
)

var State *GroupsState

type LiveKitSettings struct {
	ServerURL string
	APIKey    string
	APISecret string
}

type Options struct {
	DB        eventstore.Store
	SecretKey nostr.SecretKey
	RelayURL  string
	BaseURL   string
	LiveKit   LiveKitSettings
}

type GroupsState struct {
	Groups     *xsync.MapOf[string, *Group]
	AllMembers *xsync.MapOf[nostr.PubKey, int]

	DB       eventstore.Store
	relayURL string
	baseURL  string
	livekit  LiveKitSettings

	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Uint32

	secretKey nostr.SecretKey
}

func NewGroupsState(opts Options) *GroupsState {
	state := &GroupsState{
		Groups:     xsync.NewMapOf[string, *Group](),
		AllMembers: xsync.NewMapOf[nostr.PubKey, int](),
		DB:         opts.DB,
		relayURL:   opts.RelayURL,
		baseURL:    opts.BaseURL,
		livekit:    opts.LiveKit,
		secretKey:  opts.SecretKey,
	}

	if err := state.loadGroupsFromDB(); err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}

func (s *GroupsState) UpdateRuntimeConfig(relayURL string, baseURL string, livekit LiveKitSettings) {
	s.relayURL = relayURL
	s.baseURL = baseURL
	s.livekit = livekit
}

func (s *GroupsState) HandleEventSaved(ctx context.Context, event nostr.Event) {
	for _, affectedGroup := range s.ProcessEvent(ctx, event) {
		for updated := range s.SyncGroupMetadataEvents(affectedGroup) {
			global.R.BroadcastEvent(updated)
		}
	}

	if group := s.GetGroupFromEvent(event); group != nil {
		if err := group.IndexEvent(event); err != nil {
			L.Warn().Err(err).Str("group", group.Address.ID).Msg("failed to index event")
		}
	}
}

func (s *GroupsState) RequestAuthWhenNecessary(
	ctx context.Context,
	filter nostr.Filter,
) (reject bool, msg string) {
	authed := khatru.GetAllAuthed(ctx)
	groupIds, _ := filter.Tags["h"]
	if len(groupIds) == 0 {
		groupIds, _ = filter.Tags["d"]
	}

	for _, groupId := range groupIds {
		if group, ok := s.Groups.Load(groupId); ok {
			if group.Private {
				if len(authed) == 0 {
					return true, "auth-required: you're trying to access a private group"
				} else if !group.AnyOfTheseIsAMember(authed) {
					return true, "restricted: you're trying to access a group of which you're not a member"
				}
			}
		}
	}

	return false, ""
}
