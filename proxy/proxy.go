package proxy

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/internal"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const CalendarContentType = "text/calendar"

// TTLFor returns the TTL for the given url.URL. It uses the hostname
// to search through config.Config IcalTTLMap.
func TTLFor(uri *url.URL, cfg config.Config) time.Duration {
	// Given a url hostname of foo.example.org, we want to match against ICAL_TTL_EXAMPLEORG and ICAL_TTL_FOOEXAMPLEORG
	// Given a url hostname of example.org, we want to match against ICAL_TTL_EXAMPLEORG
	// So check to see that the url hostname ends with the 'env var hostname'
	cleanHostname := NormalizeHostname(uri)
	for envHostname, d := range cfg.IcalTTLMap {
		// FOOEXAMPLEORG, EXAMPLEORG, etc check to match they end with EXAMPLEORG
		if strings.HasSuffix(cleanHostname, envHostname) {
			return d
		}
	}
	return config.IcalBaseTTL
}

// NormalizeHostname returns the url's hostname, normalized for TTL matches.
// So https://example.org/foo would become "EXAMPLEORG", etc.
func NormalizeHostname(u *url.URL) string {
	h := strings.ToUpper(cleanHostname.ReplaceAllString(u.Hostname(), ""))
	return h
}

var cleanHostname = regexp.MustCompile("[^a-zA-Z0-9]")

type Feed struct {
	Body      []byte
	MD5       string
	FetchedAt time.Time
}

func Fetch(ctx context.Context, url *url.URL) (*Feed, error) {
	now := time.Now().Truncate(time.Second)
	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", config.UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, internal.ErrWrap(err, "feed fetch failed reading body")
	}
	if resp.StatusCode != 200 {
		return nil, &OriginError{
			StatusCode: resp.StatusCode,
			Body:       b,
		}
	}
	f := &Feed{}
	f.Body = b
	f.FetchedAt = now
	hash := md5.Sum(b)
	f.MD5 = hex.EncodeToString(hash[:])
	return f, nil
}

var httpClient *http.Client

func init() {
	httpClient = &http.Client{
		Timeout: time.Minute,
	}
}

// OriginError is used where the upstream origin server returned an error when fetching a feed.
type OriginError struct {
	StatusCode int
	Body       []byte
}

func (e *OriginError) Error() string {
	return fmt.Sprintf("upstream error: status=%d body=%s", e.StatusCode, string(e.Body))
}
