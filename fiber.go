package debugbar

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
)

//go:embed assets
var assets embed.FS

// Options is how the app fits the bar in.
type Options struct {
	// Skip leaves requests out (static files, ...): nothing recorded.
	Skip func(c fiber.Ctx) bool
	// Notes, after the handler: things to show (e.g. the session's data).
	Notes func(c fiber.Ctx) map[string]any
}

// prefix is where the bar's own files and the stored profiles are.
const prefix = "/_debugbar/"

// Fiber is the middleware: early in the chain, before anything that sets
// the request's context (each later context derives from this one), and
// after compression (the bar is injected first, then the page compressed).
func Fiber(o Options) fiber.Handler {
	installed.Store(true)
	profiles := &store{}
	return func(c fiber.Ctx) error {
		path := c.Path()
		if strings.HasPrefix(path, prefix) {
			return serve(c, profiles, strings.TrimPrefix(path, prefix))
		}
		if o.Skip != nil && o.Skip(c) {
			return c.Next()
		}

		p := newProfile(c.Method(), path)
		c.SetContext(with(c.Context(), p))
		if err := c.Next(); err != nil {
			// the app's error page now, so that its status (and the bar) are in it
			if err := c.App().ErrorHandler(c, err); err != nil {
				return err
			}
		}
		if o.Notes != nil {
			for k, v := range o.Notes(c) {
				p.Notes = append(p.Notes, Labeled{Key: k, Value: v})
			}
		}
		route := ""
		if r := c.Route(); r != nil {
			route = r.Path
		}
		p.finish(c.Response().StatusCode(), route)
		profiles.put(p)
		c.Set("X-Debugbar", p.ID)

		if strings.HasPrefix(string(c.Response().Header.ContentType()), "text/html") {
			inject(c, p)
		}
		return nil
	}
}

// watchAjax goes first in <head>, before the page's scripts: it notes the
// X-Debugbar header of every AJAX response, for bar.js (loaded at the end).
const watchAjax = `<script>(function(){var q=window.__debugbarAjax=[];function seen(id){if(id){q.push(id);if(window.__debugbarOnAjax)window.__debugbarOnAjax(id)}}` +
	`var send=XMLHttpRequest.prototype.send;XMLHttpRequest.prototype.send=function(){this.addEventListener("loadend",function(){seen(this.getResponseHeader("X-Debugbar"))});return send.apply(this,arguments)};` +
	`var f=window.fetch;if(f){window.__debugbarFetch=f;window.fetch=function(){return f.apply(this,arguments).then(function(r){seen(r.headers.get("X-Debugbar"));return r})}}})();</script>`

// inject puts the AJAX watcher after the page's <head> and the bar before
// its </body>.
func inject(c fiber.Ctx, p *Profile) {
	body := c.Response().Body()
	i := bytes.LastIndex(body, []byte("</body>"))
	if i < 0 {
		return
	}
	h := headEnd(body[:i])
	p.mu.Lock()
	data, err := json.Marshal(p) // escapes <, > and &: safe in a script
	p.mu.Unlock()
	if err != nil {
		return
	}
	var out bytes.Buffer
	out.Grow(len(body) + len(data) + len(watchAjax) + 200)
	if h >= 0 {
		out.Write(body[:h])
		out.WriteString(watchAjax)
		out.Write(body[h:i])
	} else {
		out.Write(body[:i])
	}
	out.WriteString(`<script id="__debugbar-data" type="application/json">`)
	out.Write(data)
	out.WriteString(`</script><script src="` + prefix + `bar.js" defer></script>`)
	out.Write(body[i:])
	c.Response().SetBodyRaw(out.Bytes())
}

// headEnd is where the <head> tag (with or without attributes, not
// <header>) ends, or -1.
func headEnd(body []byte) int {
	for from := 0; ; {
		i := bytes.Index(body[from:], []byte("<head"))
		if i < 0 {
			return -1
		}
		i += from + len("<head")
		if i < len(body) && (body[i] == '>' || body[i] == ' ') {
			if j := bytes.IndexByte(body[i:], '>'); j >= 0 {
				return i + j + 1
			}
			return -1
		}
		from = i
	}
}

func serve(c fiber.Ctx, profiles *store, name string) error {
	switch {
	case name == "bar.js" || name == "bar.css":
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}
		c.Type(strings.TrimPrefix(name, "bar."), "utf-8")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.Send(b)
	case strings.HasSuffix(name, ".json"):
		p := profiles.get(strings.TrimSuffix(name, ".json"))
		if p == nil {
			return c.SendStatus(fiber.StatusNotFound)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return c.JSON(p)
	}
	return c.SendStatus(fiber.StatusNotFound)
}
