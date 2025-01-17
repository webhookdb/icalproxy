package types

import "time"

// NormalizedHostname would be "EXAMPLEORG" for "example.org".
type NormalizedHostname string

// MD5Hash is the hex of an MD5 hash.
type MD5Hash string

// TTL is the time-to-live is some expiration interval.
type TTL time.Duration
