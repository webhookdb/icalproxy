package feed

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/types"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CalendarContentType = "text/calendar"

// DefaultTTL is a general purpose slow TTL we use as a fallback
// for calendars that don't match more specific, faster TTLs.
// This is a constant, not configurable, since we don't want it to change
// and isn't really at the discretion of the operator.
const DefaultTTL = types.TTL(2 * time.Hour)

// TTLFor returns the TTL for the given url.URL. It uses the hostname
// to search through config.Config IcalTTLMap.
func TTLFor(uri *url.URL, ttlMap map[types.NormalizedHostname]types.TTL) types.TTL {
	// Given a url hostname of foo.example.org, we want to match against ICAL_TTL_EXAMPLEORG and ICAL_TTL_FOOEXAMPLEORG
	// Given a url hostname of example.org, we want to match against ICAL_TTL_EXAMPLEORG
	// So check to see that the url hostname ends with the 'env var hostname'
	cleanHostname := types.NormalizeURLHostname(uri)
	result := DefaultTTL
	for envHostname, d := range ttlMap {
		// FOOEXAMPLEORG, EXAMPLEORG, etc check to match they end with EXAMPLEORG
		if strings.HasSuffix(string(cleanHostname), string(envHostname)) {
			if d < result {
				result = d
			}
		}
	}
	return result
}

type Feed struct {
	Url         *url.URL
	HttpHeaders map[string]string
	HttpStatus  int
	Body        []byte
	MD5         types.MD5Hash
	FetchedAt   time.Time
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
	f := New(url, internal.HeaderMap(resp.Header), resp.StatusCode, b, now)
	return f, nil
}

func New(url *url.URL, headers map[string]string, httpStatus int, body []byte, fetchedAt time.Time) *Feed {
	f := &Feed{
		Url:         url,
		HttpHeaders: headers,
		HttpStatus:  httpStatus,
		Body:        body,
		FetchedAt:   fetchedAt,
	}
	hash := md5.Sum(body)
	f.MD5 = types.MD5Hash(hex.EncodeToString(hash[:]))
	return f
}

var httpClient *http.Client

func init() {
	httpClient = &http.Client{
		Timeout: time.Minute,
	}
}
