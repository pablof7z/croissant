package groups

import (
	crand "crypto/rand"
	"encoding/base64"
	"io"
)

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := io.ReadFull(crand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
