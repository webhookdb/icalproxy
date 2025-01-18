package internal

import (
	"fmt"
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
