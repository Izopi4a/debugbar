package debugbar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestSpanAndNoteDoNothingUntilInstalled(t *testing.T) {
	if installed.Load() {
		t.Skip("installed by another test")
	}
	p := newProfile("GET", "/")
	ctx := with(context.Background(), p)
	Span(ctx, "render")()
	Note(ctx, "k", 1)
	if len(p.Spans)+len(p.Notes) != 0 {
		t.Errorf("recorded without the middleware: %+v", p)
	}
}

func TestMiddleware(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		c.Status(500).Type("html")
		return c.SendString("<html><body>error page</body></html>")
	}})
	app.Use(Fiber(Options{
		Skip:  func(c fiber.Ctx) bool { return strings.HasPrefix(c.Path(), "/css/") },
		Notes: func(fiber.Ctx) map[string]any { return map[string]any{"session": map[string]int{"basket": 2}} },
	}))
	app.Get("/page", func(c fiber.Ctx) error {
		defer Span(c.Context(), "render")()
		c.Type("html")
		return c.SendString(`<html><head dir="ltr"><title>x</title></head><body><header>h</header><p>page</p></body></html>`)
	})
	app.Get("/api", func(c fiber.Ctx) error { return c.JSON(map[string]bool{"ok": true}) })
	app.Get("/fails", func(c fiber.Ctx) error { return errors.New("boom") })
	app.Get("/css/site.css", func(c fiber.Ctx) error { return c.SendString("body{}") })

	get := func(path string) (string, string, int) {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Header.Get("X-Debugbar"), resp.StatusCode
	}

	body, id, _ := get("/page")
	if id == "" || !strings.Contains(body, `<script id="__debugbar-data"`) || !strings.HasSuffix(body, "</script></body></html>") ||
		!strings.HasPrefix(body, `<html><head dir="ltr"><script>(function(){var q=window.__debugbarAjax`) {
		t.Fatalf("page: id %q, %s", id, body)
	}
	raw, _, _ := get("/_debugbar/" + id + ".json")
	var p Profile
	if err := json.Unmarshal([]byte(raw), &p); err != nil || p.Route != "/page" || p.Status != 200 || len(p.Spans) != 1 || len(p.Notes) != 1 {
		t.Errorf("profile: %v %s", err, raw)
	}

	if body, id, _ := get("/api"); id == "" || strings.Contains(body, "__debugbar") {
		t.Errorf("JSON: id %q, body %s (no bar in JSON)", id, body)
	}
	if body, id, status := get("/fails"); status != 500 || id == "" || !strings.Contains(body, "__debugbar-data") {
		t.Errorf("error page: %d %q %s", status, id, body)
	}
	if _, id, _ := get("/css/site.css"); id != "" {
		t.Errorf("a skipped path was profiled")
	}
	if js, _, status := get("/_debugbar/bar.js"); status != 200 || !strings.Contains(js, "attachShadow") {
		t.Errorf("bar.js: %d", status)
	}
	if _, _, status := get("/_debugbar/nope.json"); status != 404 {
		t.Errorf("unknown profile: %d", status)
	}
}

func TestSqlcName(t *testing.T) {
	name, sql := sqlcName("-- name: GetProduct :one\nSELECT 1\nFROM x")
	if name != "GetProduct" || sql != "SELECT 1\nFROM x" {
		t.Errorf("%q %q", name, sql)
	}
	if name, sql := sqlcName("SELECT 1"); name != "" || sql != "SELECT 1" {
		t.Errorf("%q %q", name, sql)
	}
}
