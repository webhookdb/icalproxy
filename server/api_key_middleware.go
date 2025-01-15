package server

import (
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
)

func ApiKeyMiddleware(key string) (echo.MiddlewareFunc, error) {
	if key == "" {
		return nil, errors.New("ApiKey must be configured")
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			expected := fmt.Sprintf("Apikey %s", key)
			if authHeader != expected {
				return echo.NewHTTPError(401, "Header required or incorrect: 'Authorization: Apikey [value]'")
			}
			return next(c)
		}
	}, nil
}
