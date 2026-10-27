package debugbar

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------- pgx

// PgxTracer records the queries made with a request's context:
// poolConfig.ConnConfig.Tracer = debugbar.PgxTracer{}.
type PgxTracer struct{}

type queryKey struct{}

type queryStart struct {
	sql     string
	args    []any
	started time.Time
}

func (PgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if from(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, queryKey{}, &queryStart{sql: d.SQL, args: d.Args, started: time.Now()})
}

func (PgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	p, q := from(ctx), ctx.Value(queryKey{})
	if p == nil || q == nil {
		return
	}
	s := q.(*queryStart)
	name, sql := sqlcName(s.sql)
	e := Event{Name: name, Text: sql, Rows: d.CommandTag.RowsAffected()}
	for _, a := range s.args {
		e.Args = append(e.Args, clip(fmt.Sprintf("%v", deref(a)), 200))
	}
	if d.Err != nil {
		e.Error = d.Err.Error()
	}
	p.add(&p.Queries, e, s.started)
}

// sqlcName splits sqlc's "-- name: GetProduct :one" line off a query.
func sqlcName(sql string) (string, string) {
	sql = strings.TrimSpace(sql)
	if rest, ok := strings.CutPrefix(sql, "-- name: "); ok {
		line, body, _ := strings.Cut(rest, "\n")
		name, _, _ := strings.Cut(line, " ")
		return name, strings.TrimSpace(body)
	}
	return "", sql
}

func deref(v any) any {
	switch p := v.(type) {
	case *string:
		if p != nil {
			return *p
		}
		return nil
	case *int64:
		if p != nil {
			return *p
		}
		return nil
	case *int16:
		if p != nil {
			return *p
		}
		return nil
	case *float64:
		if p != nil {
			return *p
		}
		return nil
	}
	return v
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------- go-redis

// RedisHook records the commands sent with a request's context:
// rdb.AddHook(debugbar.RedisHook{}).
type RedisHook struct{}

func (RedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (RedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		p := from(ctx)
		if p == nil {
			return next(ctx, cmd)
		}
		started := time.Now()
		err := next(ctx, cmd)
		p.add(&p.Redis, redisEvent([]redis.Cmder{cmd}, err), started)
		return err
	}
}

func (RedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		p := from(ctx)
		if p == nil {
			return next(ctx, cmds)
		}
		started := time.Now()
		err := next(ctx, cmds)
		e := redisEvent(cmds, err)
		e.Name = "pipeline"
		p.add(&p.Redis, e, started)
		return err
	}
}

func redisEvent(cmds []redis.Cmder, err error) Event {
	parts := make([]string, len(cmds))
	for i, c := range cmds {
		words := make([]string, len(c.Args()))
		for j, a := range c.Args() {
			words[j] = fmt.Sprintf("%v", a)
		}
		parts[i] = clip(strings.Join(words, " "), 300)
	}
	e := Event{Text: strings.Join(parts, "\n")}
	if err != nil && err != redis.Nil {
		e.Error = err.Error()
	}
	return e
}

// ---------------------------------------------------------------- net/http

// Transport records the HTTP calls made with a request's context (Speedy,
// Stripe, ...): http.DefaultTransport = debugbar.Transport(http.DefaultTransport).
func Transport(base http.RoundTripper) http.RoundTripper { return transport{base} }

type transport struct{ base http.RoundTripper }

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	p := from(req.Context())
	if p == nil {
		return t.base.RoundTrip(req)
	}
	started := time.Now()
	resp, err := t.base.RoundTrip(req)
	// no query string: it can hold keys
	e := Event{Name: req.Method, Text: req.URL.Scheme + "://" + req.URL.Host + req.URL.Path}
	if err != nil {
		e.Error = err.Error()
	} else {
		e.Rows = int64(resp.StatusCode)
	}
	p.add(&p.HTTP, e, started)
	return resp, err
}
