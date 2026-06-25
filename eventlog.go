package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if !accepted && reason != "" {
		line += fmt.Sprintf(" reason=%q", reason)
	}
	line += "\n"

	eventLogMu.Lock()
	eventLogFile.WriteString(line)
	eventLogMu.Unlock()
}
