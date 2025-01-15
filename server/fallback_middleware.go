package server

import (
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/webhookdb/icalproxy/appglobals"
)

var ErrFallback = errors.New("fallback")

func FallbackMiddleware(ag *appglobals.AppGlobals) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err == nil || !errors.Is(err, ErrFallback) {
				return err
			}
			eh := &endpointHandler{
				ag: ag,
				c:  c,
			}
			return eh.runAsProxy(api.StdContext(c))
		}
	}
}
