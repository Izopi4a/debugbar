// Package debugbar is a development toolbar, like Symfony's: for each
// request its time, database queries, Redis commands, outgoing HTTP calls
// and timed spans, shown in a bar at the bottom of HTML pages; AJAX requests
// made by the page are listed in it too.
//
// Only for development: the app installs it (the Fiber middleware, the pgx
// tracer, the go-redis hook, the HTTP transport) only there. Without it
// nothing is recorded, and Span and Note return after checking one flag.
//
// It knows nothing of the app using it; the four apps on it share this
// module (go get github.com/izopi4a/debugbar).
package debugbar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// installed is set by the middleware: until then Span and Note do nothing.
var installed atomic.Bool

// Profile is what was recorded for one request.
type Profile struct {
	mu    sync.Mutex
	start time.Time

	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	Ms         float64   `json:"ms"`
	Queries    []Event   `json:"queries"`
	Redis      []Event   `json:"redis"`
	HTTP       []Event   `json:"http"`
	Spans      []Event   `json:"spans"`
	Notes      []Labeled `json:"notes"`
	HeapMB     float64   `json:"heapMB"`
	Goroutines int       `json:"goroutines"`
}

// Event is a query, a Redis command, an HTTP call or a span: when it started
// (ms after the request did) and how long it took.
type Event struct {
	Name  string   `json:"name,omitempty"` // a query's sqlc name, a span's name
	Text  string   `json:"text"`           // the SQL, the command, the URL
	Args  []string `json:"args,omitempty"`
	At    float64  `json:"at"`
	Ms    float64  `json:"ms"`
	Rows  int64    `json:"rows"`
	Error string   `json:"error,omitempty"`
}

// Labeled is something the app adds (e.g. the session's data).
type Labeled struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func newProfile(method, path string) *Profile {
	b := make([]byte, 8)
	rand.Read(b)
	now := time.Now()
	return &Profile{ID: hex.EncodeToString(b), Time: now, start: now, Method: method, Path: path}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (p *Profile) add(list *[]Event, e Event, started time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e.At, e.Ms = ms(started.Sub(p.start)), ms(time.Since(started))
	*list = append(*list, e)
}

func (p *Profile) finish(status int, route string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status, p.Route, p.Ms = status, constraints.ReplaceAllString(route, ""), ms(time.Since(p.start))
	p.HeapMB = float64(m.HeapAlloc*10/(1<<20)) / 10
	p.Goroutines = runtime.NumGoroutine()
}

// constraints are a route's parameter constraints: /:lang<regex(...)> → /:lang
var constraints = regexp.MustCompile(`<[^<>]*>`)

type ctxKey struct{}

func with(ctx context.Context, p *Profile) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func from(ctx context.Context) *Profile {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(ctxKey{}).(*Profile)
	return p
}

// Span times a piece of work: defer debugbar.Span(ctx, "render")().
func Span(ctx context.Context, name string) func() {
	if !installed.Load() {
		return func() {}
	}
	p := from(ctx)
	if p == nil {
		return func() {}
	}
	started := time.Now()
	return func() { p.add(&p.Spans, Event{Name: name}, started) }
}

// Note adds something to the request's profile, shown as JSON.
func Note(ctx context.Context, key string, value any) {
	if !installed.Load() {
		return
	}
	if p := from(ctx); p != nil {
		p.mu.Lock()
		p.Notes = append(p.Notes, Labeled{Key: key, Value: value})
		p.mu.Unlock()
	}
}

// store keeps the last profiles, for the AJAX requests' (fetched by id).
type store struct {
	mu    sync.Mutex
	byID  map[string]*Profile
	order []string
}

const keep = 200

func (s *store) put(p *Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = map[string]*Profile{}
	}
	s.byID[p.ID] = p
	s.order = append(s.order, p.ID)
	if len(s.order) > keep {
		delete(s.byID, s.order[0])
		s.order = s.order[1:]
	}
}

func (s *store) get(id string) *Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}
