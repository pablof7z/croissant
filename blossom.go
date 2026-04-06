package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/blossom"

	"fiatjaf.com/croissant/fs"
	"fiatjaf.com/croissant/global"
)

var (
	blossomServer  *blossom.BlossomServer
	blossomIndex   blossom.EventStoreBlobIndexWrapper
	blossomIndexDB *mmm.IndexingLayer
	blossomFS      fs.FS
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

	// init filesystem backend
	if global.S.Blossom.S3KeyID != "" && global.S.Blossom.S3Secret != "" && global.S.Blossom.S3Bucket != "" {
		blossomFS, err = fs.NewS3FS(global.S.Blossom.S3Endpoint, global.S.Blossom.S3KeyID, global.S.Blossom.S3Secret, global.S.Blossom.S3Bucket)
		if err != nil {
			return fmt.Errorf("failed to init s3 filesystem: %w", err)
		}
		L.Info().Str("endpoint", global.S.Blossom.S3Endpoint).Str("bucket", global.S.Blossom.S3Bucket).Msg("blossom using s3")
	} else {
		// if the specified path is an absolute path we respect that, maybe the user has mapped that to some remote magic, who knows
		p := filepathJoinWithAbsolute(global.E.DataPath, global.S.Blossom.LocalPath)
		blossomFS, err = fs.NewSubdirFS(p)
		if err != nil {
			return fmt.Errorf("failed to init local filesystem: %w", err)
		}
		L.Info().Str("path", p).Msg("blossom using local filesystem")
	}

	blossomIndex = blossom.EventStoreBlobIndexWrapper{
		Store:      blossomIndexDB,
		ServiceURL: serviceURL,
	}

	blossomServer = blossom.New(relay, serviceURL)
	blossomServer.Store = blossomIndex

	blossomServer.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		return blossomFS.Save(ctx, sha256+ext, body)
	}
	blossomServer.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		if global.S.Blossom.S3RedirectBaseURL != "" {
			redir, err := url.Parse(global.S.Blossom.S3RedirectBaseURL)
			if err != nil {
				L.Warn().Err(err).Str("base_url", global.S.Blossom.S3RedirectBaseURL).
					Msg("blossom redirect base url invalid")
				return nil, nil, err
			}

			redir.Path = path.Join(redir.Path, sha256+ext)
			return nil, redir, nil
		}

		reader, err := blossomFS.Open(ctx, sha256+ext)
		return reader, nil, err
	}
	blossomServer.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return blossomFS.Remove(ctx, sha256+ext)
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

func resetBlossom() {
	blossomServer = nil
	blossomIndex = blossom.EventStoreBlobIndexWrapper{}
	blossomIndexDB = nil
	blossomFS = nil
}
