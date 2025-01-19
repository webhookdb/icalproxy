package server

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/types"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func Register(_ context.Context, e *echo.Echo, ag *appglobals.AppGlobals) error {
	mw := []echo.MiddlewareFunc{FallbackMiddleware(ag)}
	if ag.Config.ApiKey != "" {
		apiKeyMw, err := ApiKeyMiddleware(ag.Config.ApiKey)
		if err != nil {
			return err
		}
		mw = append(mw, apiKeyMw)
	}
	e.HEAD("/", handle(ag), mw...)
	e.GET("/", handle(ag), mw...)
	e.GET("/stats", handleStats(ag), mw...)
	return nil
}

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
		fd, err := db.New(h.ag.DB).FetchContentsAsFeed(ctx, h.url)
		if err != nil {
			return false, ErrFallback
		}
		h.c.Response().Header().Set("Ical-Proxy-Cached", "true")
		return true, h.serveResponse(ctx, fd)
	}
	return false, nil
}

func (h *endpointHandler) refetchAndCommit(ctx context.Context) (*feed.Feed, error) {
	fd, err := feed.Fetch(ctx, h.url)
	if err != nil {
		return nil, err
	}
	// If the commit is coming through the server, we don't need to send a webhook.
	if err := db.New(h.ag.DB).CommitFeed(ctx, fd, nil); err != nil {
		logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "commit_feed_error")
	}
	return fd, err
}

func (h *endpointHandler) serveResponse(_ context.Context, fd *feed.Feed) error {
	if fd.HttpStatus >= 400 {
		// Origin errors should be 'proxied' as a 421 error.
		// If we use any error code, it makes it very confusing both operationally,
		// and also more confusing as a caller. Most of these error codes are meaningless for hosts anyway-
		// some use a 403 vs a 404 for example. So return everything as a 421 and include the original status code
		// as a header.
		h.c.Response().Header().Set("Ical-Proxy-Origin-Error", strconv.Itoa(fd.HttpStatus))
		return h.c.Blob(http.StatusMisdirectedRequest, fd.HttpHeaders["Content-Type"], fd.Body)
	}
	h.c.Response().Header().Set("Content-Type", feed.CalendarContentType)
	h.c.Response().Header().Set("Content-Length", strconv.Itoa(len(fd.Body)))
	h.c.Response().Header().Set("Etag", string(fd.MD5))
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
	resp, err := feed.Fetch(ctx, h.url)
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
