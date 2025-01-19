package internal

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/webhookdb/icalproxy/types"
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
