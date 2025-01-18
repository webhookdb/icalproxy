package fp

import "github.com/lithictech/go-aperitif/v2/convext"

func Must[T any](t T, err error) T {
	convext.Must(err)
	return t
}
