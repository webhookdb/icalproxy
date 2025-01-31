package fp

import (
	"errors"
	"github.com/lithictech/go-aperitif/v2/convext"
)

// ErrorAs returns (error, true) if errors.As returns true.
// Allows errors.As to be used in a one-liner.
func ErrorAs[T error](err error) (T, bool) {
	tgt := new(T)
	coerced := errors.As(err, tgt)
	return *tgt, coerced
}

func Must[T any](t T, err error) T {
	convext.Must(err)
	return t
}

func Map[T any, V any](sl []T, f func(r T) V) []V {
	r := make([]V, len(sl))
	for i, t := range sl {
		r[i] = f(t)
	}
	return r
}
