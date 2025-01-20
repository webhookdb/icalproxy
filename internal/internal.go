package internal

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/webhookdb/icalproxy/types"
	"io"
	"net/http"
)

func ErrWrap(e error, msg string, args ...any) error {
	return fmt.Errorf(fmt.Sprintf(msg, args...)+": %w", e)
}

func HeaderMap(h http.Header) map[string]string {
	r := make(map[string]string, len(h))
	for k, v := range h {
		r[k] = v[0]
	}
	return r
}

func MD5HashHex(b []byte) types.MD5Hash {
	hash := md5.Sum(b)
	return types.MD5Hash(hex.EncodeToString(hash[:]))
}

// ReadAllWithContext is like io.ReadAll but will cancel if ctx.Done is available,
// so is responsive to timeouts and cancelations.
func ReadAllWithContext(ctx context.Context, r io.Reader) ([]byte, error) {
	// NOTE: This code is taken directly from io.ReadAll,
	// with a select from context.Done added after each chunk read.
	// This makes sure we don't keep reading once we've canceled.
	b := make([]byte, 0, 512)
	for {
		n, err := r.Read(b[len(b):cap(b)])
		// begin custom code
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		// end custom code
		b = b[:len(b)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return b, err
		}

		if len(b) == cap(b) {
			// Add more capacity (let append pick how much).
			b = append(b, 0)[:len(b)]
		}
	}
}
