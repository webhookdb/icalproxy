package pgxt

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectToUrl opens a connection pool using the given URL.
func ConnectToUrl(url string, opts ...func(*pgxpool.Config)) (*pgxpool.Pool, error) {
	pgConnConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(pgConnConfig)
	}
	return pgxpool.NewWithConfig(context.Background(), pgConnConfig)
}

type PgErrorCode string
