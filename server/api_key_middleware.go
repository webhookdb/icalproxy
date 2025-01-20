package server

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"strings"
)

func ApiKeyMiddlewares(key string) ([]echo.MiddlewareFunc, error) {
	if key == "" {
		return nil, errors.New("ApiKey must be configured")
	}
	basicAuthMW := middleware.BasicAuthWithConfig(middleware.BasicAuthConfig{
		Skipper: func(c echo.Context) bool {
			// If the caller provided Authorization: Apikey, we'll use that;
			// otherwise, we use basic auth.
			return isApiKeyAuth(c)
		},
		Validator: func(reqUsername string, reqPassword string, c echo.Context) (bool, error) {
			passOk := strconstcmp(key, reqPassword)
			return passOk, nil
		},
	})
	apiKeyMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !isApiKeyAuth(c) {
				return next(c)
			}
			authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
			expected := fmt.Sprintf("Apikey %s", key)
			if !strconstcmp(authHeader, expected) {
				return echo.NewHTTPError(401, "Header required or incorrect: 'Authorization: Apikey [value]'")
			}
			return next(c)
		}
	}
	return []echo.MiddlewareFunc{basicAuthMW, apiKeyMW}, nil
}

func isApiKeyAuth(c echo.Context) bool {
	authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
	return strings.HasPrefix(authHeader, "Apikey")
}

func strconstcmp(s1, s2 string) bool {
	return subtle.ConstantTimeCompare([]byte(s1), []byte(s2)) == 1
}
