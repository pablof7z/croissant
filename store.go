package main

import (
	"fmt"
	"os"

	"fiatjaf.com/nostr/eventstore/mmm"
)

func initStore(dataPath string) (*mmm.MultiMmapManager, *mmm.IndexingLayer, error) {
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	mmmlogger := L.With().Str("", "mmm").Logger()
	mmmm := &mmm.MultiMmapManager{
		Dir:    dataPath,
		Logger: &mmmlogger,
	}
	if err := mmmm.Init(); err != nil {
		return nil, nil, fmt.Errorf("failed to setup mmm: %w", err)
	}

	layer, err := mmmm.EnsureLayer("main")
	if err != nil {
		mmmm.Close()
		return nil, nil, fmt.Errorf("failed to ensure 'main': %w", err)
	}

	return mmmm, layer, nil
}
