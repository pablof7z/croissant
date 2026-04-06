package fs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var _ FS = SubdirFS{}

func NewSubdirFS(path string) (FS, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory at %s", path)
	}

	return SubdirFS{path}, nil
}

type SubdirFS struct {
	subdir string
}

func (fs SubdirFS) Save(ctx context.Context, path string, data []byte) error {
	f, err := os.Create(filepath.Join(fs.subdir, path))
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

func (fs SubdirFS) Open(ctx context.Context, path string) (io.ReadSeeker, error) {
	return os.Open(filepath.Join(fs.subdir, path))
}

func (fs SubdirFS) Remove(ctx context.Context, path string) error {
	return os.Remove(filepath.Join(fs.subdir, path))
}
