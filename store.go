package main

import (
	"fmt"
	"os"
	"time"

	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/eventstore/mmm"
)

// initStore opens the mmm manager and its "main" layer. Every layer handed out
// to the rest of the program must be wrapped with gate.guard, so that
// closeStore can wait for in-flight operations before unmapping the store.
func initStore(dataPath string) (*mmm.MultiMmapManager, *storeGate, eventstore.Store, error) {
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	mmmlogger := L.With().Str("", "mmm").Logger()
	mmmm := &mmm.MultiMmapManager{
		Dir:    dataPath,
		Logger: &mmmlogger,
	}
	if err := mmmm.Init(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to setup mmm: %w", err)
	}

	layer, err := mmmm.EnsureLayer("main")
	if err != nil {
		mmmm.Close()
		return nil, nil, nil, fmt.Errorf("failed to ensure 'main': %w", err)
	}

	gate := &storeGate{}
	return mmmm, gate, gate.guard(layer), nil
}

// closeStore seals the gate, waits for in-flight store operations, then closes
// mmm (every LMDB env, the mmap, and on macOS their POSIX semaphores). If
// operations are still running after timeout it leaves mmm open: closing it
// under them would crash the process.
func closeStore(mmmm *mmm.MultiMmapManager, gate *storeGate, timeout time.Duration) error {
	if err := gate.Close(timeout); err != nil {
		return fmt.Errorf("store left open: %w", err)
	}
	mmmm.Close()
	return nil
}
