package internal

import "fmt"

func EWrap(e error, msg string, args ...any) error {
	return fmt.Errorf(msg+": %w", append(args, e))
}
