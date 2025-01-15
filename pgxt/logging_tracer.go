package pgxt

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"time"
)

type LoggingTracerCtxKey string

const Ignore LoggingTracerCtxKey = "pgxt.loggingtracer.ignore"
const loggingTracerStartData LoggingTracerCtxKey = "pgxt.loggingtracer.startdata"

func NewLoggingTracer() *LoggingTracer {
	return &LoggingTracer{}
}

func LogIgnore(ctx context.Context) context.Context {
	return context.WithValue(ctx, Ignore, true)
}

func LogUnignore(ctx context.Context) context.Context {
	return context.WithValue(ctx, Ignore, false)
}

// LoggingTracer logs all SQL statements to the logger in the context, using logctx.Logger.
// Or add Ignore into the context to skip logging, like when calling River.
// Until River supports some alternative context factory or similar,
// there's not much we can do. See https://github.com/riverqueue/river/issues/511
// for more info.
type LoggingTracer struct{}

var _ pgx.QueryTracer = &LoggingTracer{}

type loggingTracerData struct {
	data pgx.TraceQueryStartData
	at   time.Time
}

func (lt *LoggingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	d := loggingTracerData{
		data: data,
		at:   time.Now(),
	}
	return context.WithValue(ctx, loggingTracerStartData, d)
}

func (lt *LoggingTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	d := ctx.Value(loggingTracerStartData).(loggingTracerData)
	if ignore, _ := ctx.Value(Ignore).(bool); ignore {
		return
	}
	if ctx.Value(logctx.LoggerKey) == nil {
		return
	}
	logger := logctx.Logger(ctx)
	logger = logger.With(
		"sql", d.data.SQL,
		"elapsed_ms", time.Now().Sub(d.at).Milliseconds(),
	)
	if len(d.data.Args) > 0 {
		logger = logger.With("sql_args", lt.fmtArgs(d.data.Args))
	}
	if data.Err != nil {
		logger = logger.With("sql_error", data.Err)
	}
	logger.DebugContext(ctx, "sql_query")
}

func (lt *LoggingTracer) fmtArgs(args []any) []any {
	r := make([]any, len(args))
	for i, arg := range args {
		var s string
		if b, ok := arg.([]byte); ok {
			s = fmt.Sprintf("[%d bytes]", len(b))
		} else if s2, ok := arg.(string); ok {
			s = s2
		} else {
			s = fmt.Sprintf("%v", arg)
		}
		if len(s) > 200 {
			s = fmt.Sprintf("%s...(%d chars)", s[:200], len(s)-200)
		}
		r[i] = s
	}
	return r
}
