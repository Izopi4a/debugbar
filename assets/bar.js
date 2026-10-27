// The debug bar (internal/debugbar): the page's profile is in
// #__debugbar-data; AJAX requests' profiles are fetched by the X-Debugbar
// header of their responses. Drawn in a shadow DOM: the page's CSS and the
// bar's do not touch.
(function () {
  "use strict";
  var data = document.getElementById("__debugbar-data");
  if (!data || window.__debugbar) return;
  window.__debugbar = true;

  var page = JSON.parse(data.textContent);
  var ajax = [];        // newest first
  var current = page;   // the profile the panels show
  var openPanel = null;
  var HIDDEN = "debugbar-hidden";

  var host = document.createElement("div");
  host.id = "__debugbar";
  document.body.appendChild(host);
  var root = host.attachShadow({ mode: "open" });
  root.innerHTML = '<link rel="stylesheet" href="/_debugbar/bar.css"><div class="panel" hidden></div><div class="bar"></div>';
  var bar = root.querySelector(".bar");
  var panel = root.querySelector(".panel");

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
  }
  function total(list) { return (list || []).reduce(function (t, e) { return t + e.ms; }, 0); }
  function fmt(ms) { return ms < 1 ? ms.toFixed(2) : ms < 10 ? ms.toFixed(1) : String(Math.round(ms)); }
  function statusClass(s) { return s >= 500 ? "bad" : s >= 400 ? "warn" : s >= 300 ? "info" : "ok"; }
  // identical queries (same SQL, same arguments): the second is work done twice
  function same(q) { return q.text + "\u0000" + (q.args || []).join("\u0000"); }
  function repeats(queries) {
    var n = {};
    (queries || []).forEach(function (q) { n[same(q)] = (n[same(q)] || 0) + 1; });
    return n;
  }
  function hidden() { try { return localStorage.getItem(HIDDEN) === "1"; } catch (e) { return false; } }
  function setHidden(v) { try { localStorage.setItem(HIDDEN, v ? "1" : "0"); } catch (e) {} }

  function item(name, html, cls) {
    return '<button class="item ' + (cls || "") + (openPanel === name ? " active" : "") + '" data-panel="' + name + '">' + html + "</button>";
  }

  function drawBar() {
    var p = current;
    if (hidden()) {
      bar.className = "bar mini";
      bar.innerHTML = '<button class="item" data-show="1" title="Show the debug bar">◆ ' + fmt(page.ms) + " ms</button>";
      return;
    }
    bar.className = "bar";
    var rep = repeats(p.queries), dup = 0;
    Object.keys(rep).forEach(function (k) { if (rep[k] > 1) dup += rep[k] - 1; });
    var h = "";
    if (p !== page) h += '<button class="item back" data-back="1" title="Back to the page">◀ page</button>';
    h += item("request", '<b class="st ' + statusClass(p.status) + '">' + p.status + "</b> " + esc(p.method) + " " + esc(p.route || p.path), "route");
    h += item("request", "⏱ <b>" + fmt(p.ms) + "</b> ms");
    h += item("sql", "SQL <b>" + (p.queries || []).length + "</b> · " + fmt(total(p.queries)) + " ms" + (dup ? ' <span class="badge">' + dup + " repeated</span>" : ""), dup || (p.queries || []).length > 15 ? "warn" : "");
    h += item("redis", "Redis <b>" + (p.redis || []).length + "</b> · " + fmt(total(p.redis)) + " ms");
    if ((p.http || []).length) h += item("http", "HTTP <b>" + p.http.length + "</b> · " + fmt(total(p.http)) + " ms", "info");
    if (ajax.length) h += item("ajax", "AJAX <b>" + ajax.length + "</b>", "info");
    h += '<span class="item plain" title="Go heap / goroutines">Go ' + p.heapMB + " MB · " + p.goroutines + " gr</span>";
    h += '<button class="item close" data-hide="1" title="Hide">×</button>';
    bar.innerHTML = h;
  }

  function table(head, rows) {
    return '<table><thead><tr>' + head.map(function (x) { return "<th>" + x + "</th>"; }).join("") + "</tr></thead><tbody>" + rows.join("") + "</tbody></table>";
  }

  function timeline(p) {
    var events = [];
    (p.queries || []).forEach(function (e) { events.push(["sql", e.name || "query", e]); });
    (p.redis || []).forEach(function (e) { events.push(["redis", (e.name ? "pipeline " : "") + e.text.split(" ")[0], e]); });
    (p.http || []).forEach(function (e) { events.push(["http", e.name + " " + e.text.replace(/^https?:\/\//, ""), e]); });
    (p.spans || []).forEach(function (e) { events.push(["span", e.name, e]); });
    events.sort(function (a, b) { return a[2].at - b[2].at; });
    var span = Math.max(p.ms, 0.001);
    return '<div class="timeline">' + events.map(function (ev) {
      var e = ev[2], left = Math.min(100, e.at / span * 100), width = Math.max(0.4, e.ms / span * 100);
      return '<div class="tl-row"><span class="tl-label">' + esc(ev[1]) + '</span><span class="tl-track"><span class="tl-bar ' + ev[0] + '" style="left:' + left + "%;width:" + Math.min(width, 100 - left) + '%"></span></span><span class="tl-ms">' + fmt(e.ms) + "</span></div>";
    }).join("") + "</div>";
  }

  function drawPanel() {
    var p = current, h = "";
    if (!openPanel) { panel.hidden = true; return; }
    if (openPanel === "request") {
      h += "<h3>Request</h3>" + table(["", ""], [
        "<tr><td>URL</td><td>" + esc(p.method + " " + p.path) + "</td></tr>",
        "<tr><td>Route</td><td>" + esc(p.route) + "</td></tr>",
        '<tr><td>Status</td><td><b class="st ' + statusClass(p.status) + '">' + p.status + "</b></td></tr>",
        "<tr><td>Time</td><td>" + fmt(p.ms) + " ms (SQL " + fmt(total(p.queries)) + ", Redis " + fmt(total(p.redis)) + ", HTTP " + fmt(total(p.http)) + ")</td></tr>",
        "<tr><td>At</td><td>" + esc(new Date(p.time).toLocaleTimeString()) + "</td></tr>"
      ]);
      h += "<h3>Timeline</h3>" + timeline(p);
      (p.notes || []).forEach(function (n) { h += "<h3>" + esc(n.key) + "</h3><pre>" + esc(JSON.stringify(n.value, null, 2)) + "</pre>"; });
    } else if (openPanel === "sql") {
      var rep = repeats(p.queries);
      h += "<h3>SQL queries</h3>" + table(["#", "at", "ms", "rows", "query"], (p.queries || []).map(function (q, i) {
        return "<tr" + (q.error ? ' class="err"' : "") + "><td>" + (i + 1) + "</td><td>" + fmt(q.at) + "</td><td>" + fmt(q.ms) + "</td><td>" + q.rows + "</td><td>" +
          (q.name ? "<b>" + esc(q.name) + "</b> " : "") + (rep[same(q)] > 1 ? '<span class="badge">×' + rep[same(q)] + " identical</span>" : "") +
          "<details><summary>" + esc(q.text.split("\n")[0]) + "</summary><pre>" + esc(q.text) + "</pre></details>" +
          (q.args && q.args.length ? '<div class="args">' + q.args.map(function (a, j) { return "$" + (j + 1) + " = " + esc(a); }).join("<br>") + "</div>" : "") +
          (q.error ? '<div class="args">' + esc(q.error) + "</div>" : "") + "</td></tr>";
      }));
    } else if (openPanel === "redis") {
      h += "<h3>Redis commands</h3>" + table(["#", "at", "ms", "command"], (p.redis || []).map(function (r, i) {
        return "<tr" + (r.error ? ' class="err"' : "") + "><td>" + (i + 1) + "</td><td>" + fmt(r.at) + "</td><td>" + fmt(r.ms) + "</td><td><pre>" + esc(r.text) + "</pre>" + (r.error ? '<div class="args">' + esc(r.error) + "</div>" : "") + "</td></tr>";
      }));
    } else if (openPanel === "http") {
      h += "<h3>Outgoing HTTP</h3>" + table(["#", "at", "ms", "status", "call"], (p.http || []).map(function (r, i) {
        return "<tr" + (r.error ? ' class="err"' : "") + "><td>" + (i + 1) + "</td><td>" + fmt(r.at) + "</td><td>" + fmt(r.ms) + "</td><td>" + (r.rows || "") + "</td><td>" + esc(r.name + " " + r.text) + (r.error ? '<div class="args">' + esc(r.error) + "</div>" : "") + "</td></tr>";
      }));
    } else if (openPanel === "ajax") {
      h += "<h3>AJAX requests (click one to see it)</h3>" + table(["at", "status", "request", "ms", "SQL", "Redis"], ajax.map(function (a, i) {
        return '<tr class="pick" data-ajax="' + i + '"><td>' + esc(new Date(a.time).toLocaleTimeString()) + '</td><td><b class="st ' + statusClass(a.status) + '">' + a.status + "</b></td><td>" + esc(a.method + " " + a.path) + "</td><td>" + fmt(a.ms) + "</td><td>" + (a.queries || []).length + "</td><td>" + (a.redis || []).length + "</td></tr>";
      }));
    }
    panel.innerHTML = h;
    panel.hidden = false;
  }

  root.addEventListener("click", function (ev) {
    var t = ev.target.closest("[data-panel],[data-hide],[data-show],[data-back],[data-ajax]");
    if (!t) return;
    if (t.dataset.hide) { setHidden(true); openPanel = null; }
    else if (t.dataset.show) { setHidden(false); }
    else if (t.dataset.back) { current = page; }
    else if (t.dataset.ajax) { current = ajax[+t.dataset.ajax]; openPanel = "request"; }
    else { openPanel = openPanel === t.dataset.panel ? null : t.dataset.panel; }
    drawBar(); drawPanel();
  });

  // AJAX: every response of this app carries X-Debugbar (its profile's id);
  // the watcher in <head> (debugbar.watchAjax) collects them from the start
  var realFetch = window.__debugbarFetch || window.fetch;
  function load(id) {
    realFetch.call(window, "/_debugbar/" + id + ".json").then(function (r) { return r.ok ? r.json() : null; }).then(function (p) {
      if (!p) return;
      ajax.unshift(p);
      drawBar();
      if (openPanel === "ajax") drawPanel();
    });
  }
  (window.__debugbarAjax || []).forEach(load);
  window.__debugbarOnAjax = load;

  drawBar();
})();
