package fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var _ FS = S3FS{}

func NewS3FS(
	endpoint string,
	keyId string,
	secret string,
	bucket string,
) (FS, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(keyId, secret, ""),
		Secure: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to s3 %s: %w", endpoint, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	if ok, err := minioClient.BucketExists(ctx, bucket); err != nil {
		return nil, fmt.Errorf("failed to contact s3 at %s: %w", endpoint, err)
	} else if !ok {
		return nil, fmt.Errorf("bucket '%s' doesn't exist", bucket)
	}

	return S3FS{minioClient, bucket}, nil
}

type S3FS struct {
	client *minio.Client
	bucket string
}

func (fs S3FS) Save(ctx context.Context, path string, data []byte) error {
	objectExists := false
	for range fs.client.ListObjects(ctx, fs.bucket, minio.ListObjectsOptions{
		Prefix: path,
	}) {
		objectExists = true
	}
	if objectExists {
		return nil
	}

	_, err := fs.client.PutObject(ctx, fs.bucket, path, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	return err
}

func (fs S3FS) Open(ctx context.Context, path string) (io.ReadSeeker, error) {
	return fs.client.GetObject(ctx, fs.bucket, path, minio.GetObjectOptions{})
}

func (fs S3FS) Remove(ctx context.Context, path string) error {
	return fs.client.RemoveObject(ctx, fs.bucket, path, minio.RemoveObjectOptions{
		ForceDelete: true,
	})
}
