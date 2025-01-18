package types

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// MD5Hash is the hex of an MD5 hash.
type MD5Hash string

// TTL is the time-to-live is some expiration interval.
type TTL time.Duration

// NormalizedHostname would be "EXAMPLEORG" for "example.org".
type NormalizedHostname string

// NormalizeURLHostname returns the url's hostname, normalized for TTL matches.
// So https://example.org/foo would become "EXAMPLEORG", etc.
func NormalizeURLHostname(u *url.URL) NormalizedHostname {
	return NormalizeHostname(u.Hostname())
}

func NormalizeHostname(hostname string) NormalizedHostname {
	h := strings.ToUpper(cleanHostname.ReplaceAllString(hostname, ""))
	return NormalizedHostname(h)
}

// Reverse reverses the hostname string.
// Note that 'reversing a string' is really confusing because of glyphs,
// but since we are just dealing with hostnames, and for bytewise indexing,
// we can just reverse bytes.
func (h NormalizedHostname) Reverse() NormalizedHostname {
	b := []byte(h)
	r := make([]byte, len(b))
	for i, c := range b {
		r[len(b)-i-1] = c
	}
	return NormalizedHostname(r)
}

var cleanHostname = regexp.MustCompile("[^a-zA-Z0-9]")

func FormatHttpTime(t time.Time) string {
	return t.UTC().Format(http.TimeFormat)
}
