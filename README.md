# debugbar

A development toolbar for Go web apps on [Fiber v3](https://github.com/gofiber/fiber),
like Symfony's: for each request its time, the database queries (pgx; sqlc's
query names), the Redis commands (go-redis), the outgoing HTTP calls and timed
spans, shown in a bar at the bottom of every HTML page. The AJAX requests a page
makes are listed in it too (each response carries `X-Debugbar`, its profile is at
`/_debugbar/<id>.json`).

Only for development: the app installs it only there. Without it nothing is
recorded, and `Span` and `Note` return after checking one flag.

```sh
go get github.com/izopi4a/debugbar
```

## Wiring

```go
// the middleware: early in the chain, before anything that sets the request's
// context, and after compression
if dev {
	app.Use(debugbar.Fiber(debugbar.Options{
		Skip:  func(c fiber.Ctx) bool { return strings.HasPrefix(c.Path(), "/assets/") },
		Notes: func(c fiber.Ctx) map[string]any { return map[string]any{"session": sessionOf(c)} },
	}))
	poolConfig.ConnConfig.Tracer = debugbar.PgxTracer{}
	rdb.AddHook(debugbar.RedisHook{})
	httpClient.Transport = debugbar.Transport(http.DefaultTransport)
}
```

In handlers, with the request's context:

```go
defer debugbar.Span(ctx, "render")()
debugbar.Note(ctx, "basket", basket)
```

## Releasing

A tag is a release: `git tag -a v0.1.1 -m "..." && git push origin v0.1.1`, then
`go get github.com/izopi4a/debugbar@v0.1.1` in the apps.

MIT.
