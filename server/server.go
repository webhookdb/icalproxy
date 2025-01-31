package server

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/feedstorage"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/types"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// EtagBusterPrefix should be modified when existing Etag headers should be invalidated.
// This can happen for example when some related header has changed which would change
// how the response body would otherwise be treated. For example, this value was added
// when 'Content-Type' changed from 'text/calendar' to 'text/calendar; utf-8'.
// Be careful changing this, since it invalidates all existing cached responses.
const EtagBusterPrefix = "v1"

func Register(_ context.Context, e *echo.Echo, ag *appglobals.AppGlobals) error {
	e.GET("/favicon.ico", func(c echo.Context) error { return c.Blob(200, "image/x-icon", favicon) })

	mw := []echo.MiddlewareFunc{FallbackMiddleware(ag)}
	if ag.Config.ApiKey != "" {
		apiKeyMws, err := ApiKeyMiddlewares(ag.Config.ApiKey)
		if err != nil {
			return err
		}
		mw = append(mw, apiKeyMws...)
	}
	e.HEAD("/", handle(ag), mw...)
	e.GET("/", handle(ag), mw...)
	e.GET("/stats", handleStats(ag), mw...)
	return nil
}

//go:embed favicon.ico
var favicon []byte

type endpointHandler struct {
	ag  *appglobals.AppGlobals
	c   echo.Context
	url *url.URL
	row *db.FeedRow
}

func handle(ag *appglobals.AppGlobals) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := api.StdContext(c)
		eh := &endpointHandler{
			ag: ag,
			c:  c,
		}
		// Set the url. Any error here is a validation error.
		if err := eh.extractUrl(); err != nil {
			return err
		}
		ctx = logctx.AddTo(ctx, "feed_url", eh.url.String())
		// Load the row from the database, if there is one.
		// If there isn't, 'row' will be nil.
		if err := eh.loadRow(ctx); err != nil {
			return err
		}
		// See if the passed headers allow us to avoid returning data to the client.
		// This will never pass if there is no row in the database for this url,
		// if headers aren't passed, and otherwise will run Etag and Last-Modified checks.
		if err := eh.conditionalGetCheck(ctx); err != nil {
			return err
		}
		// See if we can serve the feed from what's in the database,
		// by comparing when the feed was last fetched to what we think the TTL is (3 minutes for ical, etc).
		if served, err := eh.serveIfTtl(ctx); served || err != nil {
			return err
		}
		// We discover we need to fetch the feed, store it in the database.
		fd, err := eh.refetchAndCommit(ctx)
		if err != nil {
			return err
		}
		// Serve the HTTP response
		return eh.serveResponse(ctx, fd)
	}
}

func (h *endpointHandler) extractUrl() error {
	u := h.c.QueryParam("url")
	if u == "" {
		return echo.NewHTTPError(400, "'url' query param is required")
	}
	uri, err := url.Parse(u)
	if err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("'url' is invalid: %s", err.Error()))
	}
	h.url = uri
	return nil
}

func (h *endpointHandler) loadRow(ctx context.Context) error {
	r, err := db.New(h.ag.DB).FetchFeedRow(ctx, h.url)
	if err != nil {
		return ErrFallback
	}
	h.row = r
	return nil
}

func (h *endpointHandler) conditionalGetCheck(_ context.Context) error {
	if h.row == nil {
		return nil
	}
	if etag := h.c.Request().Header.Get("If-None-Match"); etag != "" {
		if string(h.row.ContentsMD5) == etag {
			return echo.NewHTTPError(http.StatusNotModified)
		}
	}
	if lastmod := h.c.Request().Header.Get("If-Modified-Since"); lastmod != "" {
		if lastmodtz, err := http.ParseTime(lastmod); err == nil {
			rowChanged := h.row.ContentsLastModified.After(lastmodtz)
			if !rowChanged {
				return echo.NewHTTPError(http.StatusNotModified)
			}
		}
	}
	return nil
}

func (h *endpointHandler) serveIfTtl(ctx context.Context) (bool, error) {
	if h.row == nil {
		return false, nil
	}
	timeSinceFetch := time.Now().Sub(h.row.ContentsLastModified)
	maxTtl := time.Duration(feed.TTLFor(h.url, h.ag.Config.IcalTTLMap))
	if timeSinceFetch <= maxTtl {
		fd, err := db.New(h.ag.DB).FetchContentsAsFeed(ctx, h.ag.FeedStorage, h.url)
		if errors.Is(err, feedstorage.ErrNotFound) {
			return false, nil
		} else if err != nil {
			return false, ErrFallback
		}
		h.c.Response().Header().Set("Ical-Proxy-Cached", "true")
		return true, h.serveResponse(ctx, fd)
	}
	return false, nil
}

func (h *endpointHandler) refetchAndCommit(ctx context.Context) (*feed.Feed, error) {
	// This codepath should be relatively rare; it means the refresher isn't keeping feeds up to date
	// as soon as their TTL expires.
	logctx.Logger(ctx).Info("refetching feed")
	timeoutctx, cancel := context.WithTimeout(ctx, time.Duration(h.ag.Config.RequestTimeout)*time.Second)
	defer cancel()
	var previousHeaders feed.HeaderMap
	if h.row != nil {
		previousHeaders = h.row.FetchHeaders
	}
	fd, err := feed.Fetch(timeoutctx, h.url, previousHeaders)
	if err != nil && !errors.Is(err, feed.ErrNotModified) {
		return nil, err
	} else if errors.Is(err, feed.ErrNotModified) {
		// If origin told us there are no changes, we need to commit the feed to reset its TTL,
		// and then serve whatever is in cache.
		dbo := db.New(h.ag.DB)
		if err := dbo.CommitUnchanged(ctx, fd); err != nil {
			logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "commit_unchanged_feed_error")
		}
		fd, err := dbo.FetchContentsAsFeed(ctx, h.ag.FeedStorage, h.url)
		if err != nil {
			return nil, ErrFallback
		} else if fd.Body == nil {
			logctx.Logger(ctx).ErrorContext(ctx, "unchanged_feed_body_empty")
			if err := dbo.ExpireFeed(ctx, h.url); err != nil {
				return nil, internal.ErrWrap(err, "expiring feed")
			}
			return h.refetchAndCommit(ctx)
		}
		return fd, nil
	}

	// If the commit is coming through the server, we don't need to send a webhook.
	// Note that we don't compare the feed to the database version like refresher does and CommitUnchanged;
	// this code path should be relatively rare, since refresher should take care of keeping feeds up to date.
	if err := db.New(h.ag.DB).CommitFeed(ctx, h.ag.FeedStorage, fd, nil); err != nil {
		logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "commit_feed_error")
	}
	return fd, nil
}

func (h *endpointHandler) serveResponse(_ context.Context, fd *feed.Feed) error {
	if fd.HttpStatus >= 400 {
		// Origin errors should be 'proxied' as a 421 error.
		// If we use any error code, it makes it very confusing both operationally,
		// and also more confusing as a caller. Most of these error codes are meaningless for hosts anyway-
		// some use a 403 vs a 404 for example. So return everything as a 421 and include the original status code
		// as a header.
		h.c.Response().Header().Set("Ical-Proxy-Origin-Error", strconv.Itoa(fd.HttpStatus))
		contentType := fd.HttpHeaders["Content-Type"]
		if contentType == "" {
			contentType = "text/plain"
		}
		return h.c.Blob(http.StatusMisdirectedRequest, contentType, fd.Body)
	}
	h.c.Response().Header().Set("Content-Type", feed.CalendarContentType)
	h.c.Response().Header().Set("Content-Length", strconv.Itoa(len(fd.Body)))
	h.c.Response().Header().Set("Etag", EtagBusterPrefix+string(fd.MD5))
	h.c.Response().Header().Set("Last-Modified", types.FormatHttpTime(fd.FetchedAt))
	if h.c.Request().Method == http.MethodHead {
		h.c.Response().WriteHeader(200)
		return nil
	}
	return h.c.Blob(200, feed.CalendarContentType, fd.Body)
}

func (h *endpointHandler) runAsProxy(ctx context.Context) error {
	if err := h.extractUrl(); err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(h.ag.Config.RequestMaxTimeout)*time.Second)
	defer cancel()
	resp, err := feed.Fetch(timeoutCtx, h.url, nil)
	if err != nil {
		return err
	}
	h.c.Response().Header().Set("Ical-Proxy-Fallback", "true")
	return h.serveResponse(ctx, resp)
}

func handleStats(ag *appglobals.AppGlobals) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := api.StdContext(c)
		countStart := time.Now()
		refreshRowCnt, err := refresher.New(ag).CountRowsAwaitingRefresh(ctx)
		if err != nil {
			logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "counting_rows_awaiting_refresh")
			refreshRowCnt = -1
		}
		countLatency := time.Since(countStart)
		whRowCnt, err := pgxt.GetScalar[int64](ctx, ag.DB, "SELECT count(1) FROM icalproxy_feeds_v1 WHERE webhook_pending")
		if err != nil {
			logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "counting_rows_pending_webhook")
			whRowCnt = -1
		}
		resp := map[string]any{
			"pending_refresh_count": refreshRowCnt,
			"db_count_latency":      countLatency.Seconds(),
			"pending_webhooks":      whRowCnt,
		}
		return c.JSON(http.StatusOK, resp)
	}
}
