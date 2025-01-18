package pgxt

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IBegin interface {
	Begin(context.Context) (pgx.Tx, error)
}

type IExec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type IQueryRow interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type IQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

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

// GetScalars returns a slice like []int{123, 123} from the query "SELECT 123 FROM generate_series(0,1)".
func GetScalars[T any](ctx context.Context, db IQuery, stmt string, args ...any) ([]T, error) {
	rows, err := db.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows[T](rows, func(r pgx.CollectableRow) (T, error) {
		var v T
		err := r.Scan(&v)
		return v, err
	})
}

// GetScalar returns a value like int(123) from the query "SELECT 123".
func GetScalar[T any](ctx context.Context, db IQueryRow, stmt string, args ...any) (T, error) {
	row := db.QueryRow(ctx, stmt, args...)
	var t T
	return t, row.Scan(&t)
}

func WithTransaction(ctx context.Context, db IBegin, cb func(pgx.Tx) error) error {
	if t, ok := db.(pgx.Tx); ok {
		return cb(t)
	}
	t, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			t.Rollback(ctx)
			panic(r)
		}
	}()
	cberr := cb(t)
	if cberr != nil {
		if err := t.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			return err
		}
		return cberr
	}
	if err := t.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	return nil
}
