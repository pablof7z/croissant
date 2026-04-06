package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/blossom"

	"fiatjaf.com/croissant/global"
)

var (
	blossomServer    *blossom.BlossomServer
	blossomIndex     blossom.EventStoreBlobIndexWrapper
	blossomIndexDB   *mmm.IndexingLayer
	blossomFilesPath string
)

func initBlossom(relay *khatru.Relay, serviceURL string) error {
	if mmmm == nil {
		return fmt.Errorf("blossom init requires mmm manager")
	}

	var err error
	blossomIndexDB, err = mmmm.EnsureLayer("blossom")
	if err != nil {
		return fmt.Errorf("failed to ensure blossom index: %w", err)
	}

	blossomFilesPath = filepath.Join(global.E.DataPath, "blossom-files")
	if err := os.MkdirAll(blossomFilesPath, 0o755); err != nil {
		return fmt.Errorf("failed to create blossom directory: %w", err)
	}

	blossomIndex = blossom.EventStoreBlobIndexWrapper{
		Store:      blossomIndexDB,
		ServiceURL: serviceURL,
	}

	blossomServer = blossom.New(relay, serviceURL)
	blossomServer.Store = blossomIndex

	blossomServer.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		return os.WriteFile(filepath.Join(blossomFilesPath, sha256+ext), body, 0644)
	}
	blossomServer.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		file, err := os.Open(filepath.Join(blossomFilesPath, sha256+ext))
		if err != nil {
			return nil, nil, err
		}
		return file, nil, nil
	}
	blossomServer.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return os.Remove(filepath.Join(blossomFilesPath, sha256+ext))
	}

	blossomServer.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if auth == nil {
			return true, "authentication required", 401
		}
		if _, exists := State.AllMembers.Load(auth.PubKey); !exists {
			return true, "only group members can upload blobs", 403
		}
		return false, "", 0
	}

	return nil
}
