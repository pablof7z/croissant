package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"

	"fiatjaf.com/croissant/global"
)

var (
	eventLogFile *os.File
	eventLogMu   sync.Mutex
)

func initEventLog() error {
	path := filepath.Join(global.E.DataPath, "events.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	eventLogFile = f
	L.Info().Str("path", path).Msg("event log opened")
	return nil
}

func logEvent(ctx context.Context, event nostr.Event, accepted bool, reason string) {
	if eventLogFile == nil {
		return
	}

	ip := khatru.GetIP(ctx)
	ua := ""
	if conn := khatru.GetConnection(ctx); conn != nil {
		ua = conn.Request.Header.Get("User-Agent")
	}

	group := ""
	if t := event.Tags.Find("h"); t != nil && len(t) >= 2 {
		group = t[1]
	}

	status := "ACCEPTED"
	if !accepted {
		status = "REJECTED"
	}

	details := nip29Details(event)
	line := fmt.Sprintf("%s %s kind=%d id=%s pubkey=%s group=%q ip=%s ua=%q",
		time.Now().UTC().Format(time.RFC3339),
		status,
		event.Kind,
		event.ID.Hex()[:12],
		event.PubKey.Hex()[:12],
		group,
		ip,
		ua,
	)
	if details != "" {
		line += " " + details
	}
	if !accepted && reason != "" {
		line += fmt.Sprintf(" reason=%q", reason)
	}
	line += "\n"

	eventLogMu.Lock()
	eventLogFile.WriteString(line)
	eventLogMu.Unlock()
}

func nip29Details(event nostr.Event) string {
	switch event.Kind {
	case nostr.KindSimpleGroupChatMessage, nostr.KindSimpleGroupThread,
		nostr.KindSimpleGroupThreadedReply, nostr.KindSimpleGroupReply:
		content := event.Content
		if len(content) > 80 {
			content = content[:80] + "…"
		}
		return fmt.Sprintf("content=%q", content)

	case nostr.KindSimpleGroupPutUser: // 9000
		var parts []string
		for tag := range event.Tags.FindAll("p") {
			if len(tag) >= 2 {
				entry := tag[1]
				if len(entry) > 12 {
					entry = entry[:12]
				}
				if len(tag) >= 3 {
					entry += "(" + strings.Join(tag[2:], ",") + ")"
				}
				parts = append(parts, entry)
			}
		}
		return "add-users=[" + strings.Join(parts, " ") + "]"

	case nostr.KindSimpleGroupRemoveUser: // 9001
		var parts []string
		for tag := range event.Tags.FindAll("p") {
			if len(tag) >= 2 {
				pk := tag[1]
				if len(pk) > 12 {
					pk = pk[:12]
				}
				parts = append(parts, pk)
			}
		}
		return "remove-users=[" + strings.Join(parts, " ") + "]"

	case nostr.KindSimpleGroupEditMetadata: // 9002
		var parts []string
		for _, tag := range event.Tags {
			if len(tag) >= 2 {
				switch tag[0] {
				case "name":
					parts = append(parts, fmt.Sprintf("name=%q", tag[1]))
				case "about":
					about := tag[1]
					if len(about) > 40 {
						about = about[:40] + "…"
					}
					parts = append(parts, fmt.Sprintf("about=%q", about))
				case "picture":
					parts = append(parts, "picture=set")
				case "closed":
					parts = append(parts, "closed=true")
				case "open":
					parts = append(parts, "closed=false")
				case "restricted":
					parts = append(parts, "restricted=true")
				case "unrestricted":
					parts = append(parts, "restricted=false")
				case "private":
					parts = append(parts, "private=true")
				case "public":
					parts = append(parts, "private=false")
				case "hidden":
					parts = append(parts, "hidden=true")
				case "visible":
					parts = append(parts, "hidden=false")
				}
			}
		}
		return "edit=[" + strings.Join(parts, " ") + "]"

	case nostr.KindSimpleGroupDeleteEvent: // 9005
		var ids []string
		for tag := range event.Tags.FindAll("e") {
			if len(tag) >= 2 {
				id := tag[1]
				if len(id) > 12 {
					id = id[:12]
				}
				ids = append(ids, id)
			}
		}
		return "delete-events=[" + strings.Join(ids, " ") + "]"

	case nostr.KindSimpleGroupCreateGroup: // 9007
		if t := event.Tags.Find("h"); t != nil && len(t) >= 2 {
			return fmt.Sprintf("new-group=%q", t[1])
		}

	case nostr.KindSimpleGroupDeleteGroup: // 9008
		return "delete-group"

	case nostr.KindSimpleGroupCreateInvite: // 9009
		if t := event.Tags.Find("code"); t != nil && len(t) >= 2 {
			return fmt.Sprintf("invite-code=%q", t[1])
		}

	case nostr.KindSimpleGroupJoinRequest: // 9021
		code := ""
		if t := event.Tags.Find("code"); t != nil && len(t) >= 2 {
			code = fmt.Sprintf(" code=%q", t[1])
		}
		return "join" + code

	case nostr.KindSimpleGroupLeaveRequest: // 9022
		return "leave"
	}
	return ""
}
