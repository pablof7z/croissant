package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"fiatjaf.com/croissant/global"
	"fiatjaf.com/nostr"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
)

var (
	livekitHTTPClient = &http.Client{}
	livekitRooms      = make(map[string]bool)
	livekitRoomsMu    sync.RWMutex
)

type tokenSourceResponse struct {
	ServerURL        string `json:"server_url"`
	ParticipantToken string `json:"participant_token"`
}

type liveKitListParticipantsResponse struct {
	Participants []struct {
		Identity string `json:"identity"`
	} `json:"participants"`
}

func livekitStatusHandler(w http.ResponseWriter, r *http.Request) {
	if State == nil {
		http.NotFound(w, r)
		return
	}

	if State.livekit.ServerURL != "" && State.livekit.APIKey != "" && State.livekit.APISecret != "" {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

func livekitAuthHandler(w http.ResponseWriter, r *http.Request) {
	if State == nil {
		http.NotFound(w, r)
		return
	}

	if State.livekit.ServerURL == "" || State.livekit.APIKey == "" || State.livekit.APISecret == "" {
		http.NotFound(w, r)
		return
	}

	groupId := r.PathValue("groupId")
	if groupId == "" {
		http.Error(w, "group id required", http.StatusBadRequest)
		return
	}

	group, ok := State.Groups.Load(groupId)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "authorization header required", http.StatusUnauthorized)
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Nostr" {
		http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
		return
	}

	eventBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		http.Error(w, "invalid base64 encoding", http.StatusUnauthorized)
		return
	}

	var event nostr.Event
	if err := event.UnmarshalJSON(eventBytes); err != nil {
		http.Error(w, "invalid event json", http.StatusUnauthorized)
		return
	}

	if !event.VerifySignature() {
		http.Error(w, "invalid event signature", http.StatusUnauthorized)
		return
	}

	if event.Kind != 27235 {
		http.Error(w, "invalid event kind", http.StatusUnauthorized)
		return
	}

	expectedURL := State.baseURL + "/.well-known/nip29/livekit/" + groupId
	uTag := event.Tags.Find("u")
	if uTag == nil || len(uTag) < 2 || uTag[1] != expectedURL {
		http.Error(w, "invalid u tag", http.StatusUnauthorized)
		return
	}

	if !group.LiveKit {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	if err := State.ensureLiveKitRoom(group); err != nil {
		http.Error(w, "failed to ensure livekit room: "+err.Error(), http.StatusInternalServerError)
		return
	}

	token := State.generateLiveKitToken(group, event.PubKey)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenSourceResponse{
		ServerURL:        State.livekit.ServerURL,
		ParticipantToken: token,
	})
}

func livekitWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if State == nil {
		http.NotFound(w, r)
		return
	}

	if State.livekit.APIKey == "" || State.livekit.APISecret == "" {
		http.NotFound(w, r)
		return
	}

	kp := auth.NewSimpleKeyProvider(State.livekit.APIKey, State.livekit.APISecret)
	event, err := webhook.ReceiveWebhookEvent(r, kp)
	if err != nil {
		http.Error(w, "invalid webhook: "+err.Error(), http.StatusUnauthorized)
		return
	}

	room := event.GetRoom()
	if room == nil {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	groupId := room.GetName()
	if groupId == "" {
		http.Error(w, "missing room name", http.StatusBadRequest)
		return
	}

	group, ok := State.Groups.Load(groupId)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if !group.LiveKit {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	defer w.WriteHeader(http.StatusNoContent)

	switch event.Event {
	case webhook.EventParticipantJoined, webhook.EventParticipantLeft:
		participants, err := State.listLiveKitParticipants(group)
		if err != nil {
			L.Printf("failed to refresh livekit participants: %v", err)
			return
		}

		group.mu.Lock()
		group.LiveKitParticipants = participants
		group.LastLiveKitParticipantsUpdate = nostr.Now()
		evt := group.ToLiveKitParticipantsEvent()
		group.mu.Unlock()

		evt.Sign(State.secretKey)
		State.DB.ReplaceEvent(evt)
		global.R.BroadcastEvent(evt)
		return
	default:
		return
	}
}

func (s *GroupsState) ensureLiveKitRoom(group *Group) error {
	if !group.LiveKit {
		return fmt.Errorf("livekit not enabled for this group")
	}

	livekitRoomsMu.RLock()
	if livekitRooms[group.Address.ID] {
		livekitRoomsMu.RUnlock()
		return nil
	}
	livekitRoomsMu.RUnlock()

	u, _ := url.Parse(fmt.Sprintf("%s/twirp/livekit.RoomService/CreateRoom", s.livekit.ServerURL))
	u.Scheme = strings.Replace(u.Scheme, "ws", "http", 1)
	reqBody, _ := json.Marshal(map[string]any{"name": group.Address.ID})
	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.generateLiveKitServerToken())

	resp, err := livekitHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		livekitRoomsMu.Lock()
		livekitRooms[group.Address.ID] = true
		livekitRoomsMu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to create room: %s", resp.Status)
}

func (s *GroupsState) listLiveKitParticipants(group *Group) ([]nostr.PubKey, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/twirp/livekit.RoomService/ListParticipants", s.livekit.ServerURL))
	u.Scheme = strings.Replace(u.Scheme, "ws", "http", 1)
	reqBody, _ := json.Marshal(map[string]any{"room": group.Address.ID})
	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.generateLiveKitServerToken())

	resp, err := livekitHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list participants: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
	}

	var response liveKitListParticipantsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	participants := make([]nostr.PubKey, 0, len(response.Participants))
	for _, participant := range response.Participants {
		if len(participant.Identity) < 64 {
			continue
		}

		pubkey, err := nostr.PubKeyFromHex(participant.Identity[0:64])
		if err != nil {
			continue
		}

		participants = nostr.AppendUnique(participants, pubkey)
	}

	return participants, nil
}

func (s *GroupsState) generateLiveKitServerToken() string {
	at := auth.NewAccessToken(s.livekit.APIKey, s.livekit.APISecret)
	at.SetVideoGrant(&auth.VideoGrant{RoomCreate: true, RoomList: true, RoomAdmin: true})

	jwt, _ := at.ToJWT()
	return jwt
}

func (s *GroupsState) generateLiveKitToken(group *Group, pubkey nostr.PubKey) string {
	at := auth.NewAccessToken(s.livekit.APIKey, s.livekit.APISecret)
	at.SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: group.Address.ID})
	at.SetIdentity(pubkey.Hex() + ":" + randomToken(2))

	jwt, _ := at.ToJWT()
	return jwt
}
