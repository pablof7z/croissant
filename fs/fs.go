package fs

import (
	"context"
	"io"
)

type FS interface {
	Save(ctx context.Context, path string, data []byte) error
	Open(ctx context.Context, path string) (io.ReadSeeker, error)
	Remove(ctx context.Context, path string) error
}
