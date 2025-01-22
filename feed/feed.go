package feed

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"github.com/pquerna/cachecontrol/cacheobject"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/types"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CalendarContentType = "text/calendar; charset=utf-8"

// DefaultTTL is a general purpose slow TTL we use as a fallback
// for calendars that don't match more specific, faster TTLs.
// This is a constant, not configurable, since we don't want it to change
// and isn't really at the discretion of the operator.
const DefaultTTL = types.TTL(2 * time.Hour)

var ErrNotModified = errors.New("feed has not been modified (cached, 304, etc)")

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

func (f *Feed) SetBody(body []byte) {
	f.Body = body
	f.MD5 = internal.MD5HashHex(body)
}

func Fetch(ctx context.Context, u *url.URL, previousHeaders HeaderMap) (*Feed, error) {
	now := time.Now().Truncate(time.Second)
	fd := &Feed{
		Url:         u,
		HttpHeaders: make(map[string]string),
		FetchedAt:   now,
	}
	if previousHeaders != nil && feedStillCached(previousHeaders, now) {
		return fd, ErrNotModified
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", config.UserAgent)
	// Some hosts (hostfully.com) require text/calendar listed specifically in the Accept header.
	// Everyone else is fine with */*.
	req.Header.Set("Accept", "text/calendar,*/*")
	// Pass conditional get headers if we have them
	if previousHeaders != nil {
		if etag, ok := previousHeaders["Etag"]; ok {
			req.Header.Set("If-None-Match", etag)
		}
		if lastMod, ok := previousHeaders["Last-Modified"]; ok {
			req.Header.Set("If-Modified-Since", lastMod)
		}
	}
	resp, err := httpClient.Do(req)
	if isOriginBasedError(err) {
		// These are timeouts, invalid hosts, etc. We should treat these like normal HTTP errors,
		// but with a special 599 status code (0 is dangerous because most people check status >= 400 for errors).
		fd.HttpStatus = 599
		fd.SetBody([]byte(err.Error()))
		return fd, nil
	} else if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotModified {
		return fd, ErrNotModified
	}
	fd.HttpStatus = resp.StatusCode
	fd.HttpHeaders = HeadersToMap(resp.Header)
	b, err := internal.ReadAllWithContext(ctx, resp.Body)
	if err != nil {
		// If reading the body fails, we need to record an error, even if the HTTP response was a success.
		if fd.HttpStatus < 400 {
			fd.HttpStatus = 599
		}
		fd.SetBody([]byte("error reading body: " + err.Error()))
		return fd, nil
	}
	fd.SetBody(b)
	return fd, nil
}

func feedStillCached(h HeaderMap, now time.Time) bool {
	date, err := http.ParseTime(h["Date"])
	if err != nil {
		return false
	}
	cc, err := cacheobject.ParseResponseCacheControl(h["Cache-Control"])
	if err != nil || cc == nil {
		return false
	}
	maxAge := cc.MaxAge
	if maxAge > maximumMaxAge {
		maxAge = maximumMaxAge
	}
	cacheUntil := date.Add(time.Duration(maxAge) * time.Second)
	return now.Before(cacheUntil)
}

// When checking Cache-Control max-age, use this as upper bound on max-age.
// There are feeds, like sports teach schedules, that may give
// immutable values (20 years, etc) that clearly are incorrect.
// Fetching new data once a day as a worst-case is not that bad.
var maximumMaxAge = cacheobject.DeltaSeconds(int32((time.Hour * 24).Seconds()))

func isOriginBasedError(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	if strings.HasPrefix(err.Error(), "x509: ") || strings.Contains(err.Error(), ": x509: ") {
		return true
	}
	return false
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

type HeaderMap map[string]string

func HeadersToMap(h http.Header) HeaderMap {
	r := make(HeaderMap, len(h))
	for k, v := range h {
		r[k] = v[0]
	}
	return r
}

type IHttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

var httpClient IHttpClient = &http.Client{}

func init() {
	httpClient = &http.Client{
		// This should be overridden by passing a context timeout, like refresher does
		Timeout: time.Minute,
	}
}

// WithHttpClient temporarily sets the http client used for fetching.
// Only use this when testing since it isn't threadsafe.
func WithHttpClient(c IHttpClient, cb func() error) error {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = c
	return cb()
}
