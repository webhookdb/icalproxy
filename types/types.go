package types

import (
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

var cleanHostname = regexp.MustCompile("[^a-zA-Z0-9]")
