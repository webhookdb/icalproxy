package fp

import (
	"github.com/lithictech/go-aperitif/v2/convext"
)

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
