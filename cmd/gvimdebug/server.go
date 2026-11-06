package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// assets are the debug page, its styles and its script.
//
//go:embed index.html app.css app.js
var assets embed.FS

// routes returns the debug page and its API.
//
//	GET  /         the page, with app.css and app.js
//	GET  /events   server-sent events: the page state, on every change
//	GET  /frame    the screen as HTML spans
//	GET  /status   the status line: running, exited, building, and so on
//	POST /key      one key spec in the body, as encodeKey reads it
//	POST /text     text in the body, typed one key at a time; newlines send Enter
//	POST /mouse    a mouseEvent as JSON
//	POST /restart  rebuild gvim and start it again
//	POST /resize   a terminal size as JSON, {"cols":120,"rows":30}
//	GET  /font     the font file given with -font, for the page to load
func (d *debugger) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", d.handleEvents)
	mux.HandleFunc("GET /frame", d.handleFrame)
	mux.HandleFunc("GET /status", d.handleStatus)
	mux.HandleFunc("POST /key", d.handleKey)
	mux.HandleFunc("POST /text", d.handleText)
	mux.HandleFunc("POST /mouse", d.handleMouse)
	mux.HandleFunc("POST /restart", d.handleRestart)
	mux.HandleFunc("POST /resize", d.handleResize)
	mux.HandleFunc("GET /font", d.handleFont)
	mux.Handle("GET /", http.FileServerFS(assets))
	return mux
}

func (d *debugger) handleFrame(w http.ResponseWriter, _ *http.Request) {
	sess := d.current()
	if sess == nil {
		// An empty frame. The status line says why there is none.
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, frameHTML(sess.Screen()))
}

func (d *debugger) handleStatus(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, d.status())
}

// handleEvents streams the page state as server-sent events. It sends the state when the
// stream opens, then again after each change, so the page redraws when gvim draws. The
// state is one JSON object on one data line, so the frame's newlines stay inside it.
func (d *debugger) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	var last string
	for {
		// Take the change channel before the snapshot. A change after the snapshot then
		// closes the channel, so the loop sends it and nothing is missed.
		changed := d.changed()
		msg, err := json.Marshal(d.snapshot())
		if err != nil {
			return
		}
		if s := string(msg); s != last {
			last = s
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		}
	}
}

// pageState is what the page shows: the status line, the screen as HTML, the terminal
// size and the cursor.
type pageState struct {
	Status string     `json:"status"`
	Frame  string     `json:"frame"`
	Cols   int        `json:"cols"`
	Rows   int        `json:"rows"`
	Cursor pageCursor `json:"cursor"`
}

// pageCursor is the cursor of the screen. Row and Col are zero-based.
type pageCursor struct {
	Row     int  `json:"row"`
	Col     int  `json:"col"`
	Visible bool `json:"visible"`
}

func (d *debugger) handleKey(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	appCursor := false
	if sess := d.current(); sess != nil {
		appCursor = sess.AppCursorKeys()
	}
	b, err := encodeKey(strings.TrimRight(body, "\r\n"), appCursor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.send(w, b)
}

func (d *debugger) handleText(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// A terminal sends a carriage return for Enter, not a line feed.
	d.send(w, []byte(strings.ReplaceAll(body, "\n", "\r")))
}

func (d *debugger) handleMouse(w http.ResponseWriter, r *http.Request) {
	var ev mouseEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b, err := mouseBytes(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.send(w, b)
}

func (d *debugger) handleRestart(w http.ResponseWriter, _ *http.Request) {
	if err := d.restart(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFont serves the font file given with -font. Without one there is no font to serve.
func (d *debugger) handleFont(w http.ResponseWriter, r *http.Request) {
	if d.font == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, d.font)
}

// Terminal sizes the page may set. They keep the screen usable and bound the work per frame.
const (
	minCols, maxCols = 10, 400
	minRows, maxRows = 4, 200
)

// sizeRequest is a terminal size in columns and rows.
type sizeRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (d *debugger) handleResize(w http.ResponseWriter, r *http.Request) {
	var req sizeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Cols < minCols || req.Cols > maxCols || req.Rows < minRows || req.Rows > maxRows {
		http.Error(w, fmt.Sprintf("size %dx%d is outside %d-%d columns and %d-%d rows",
			req.Cols, req.Rows, minCols, maxCols, minRows, maxRows), http.StatusBadRequest)
		return
	}
	if err := d.resize(req.Cols, req.Rows); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// send writes input to gvim. It answers 409 when gvim is not running.
func (d *debugger) send(w http.ResponseWriter, b []byte) {
	sess := d.current()
	if sess == nil {
		http.Error(w, "gvim is not running", http.StatusConflict)
		return
	}
	if _, err := sess.Write(b); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readBody reads a request body of up to 1 MiB.
func readBody(w http.ResponseWriter, r *http.Request) (string, error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	return string(b), err
}
