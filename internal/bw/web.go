package bw

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed webassets/*
var webAssets embed.FS

type webSnapshot struct {
	monitorData
	Statistics Statistics  `json:"statistics"`
	History    []TaskEvent `json:"history"`
	Identity   string      `json:"identity"`
}

func readWeb(ctx context.Context, root, who string) (webSnapshot, error) {
	s, e := openStoreContext(ctx, root)
	if e != nil {
		return webSnapshot{}, e
	}
	defer os.RemoveAll(s.dir)
	snap, e := s.load()
	if e != nil {
		return webSnapshot{}, e
	}
	if snap.tip == "" {
		return webSnapshot{}, fail(1, "State is not initialized. Run bw init.")
	}
	now := time.Now().UTC()
	data, e := monitorSnapshot(snap, now)
	if e != nil {
		return webSnapshot{}, e
	}
	data.Project = filepath.Base(root)
	data.Tasks = addLinks(data.Tasks, snap.catalog, root).(TaskList)
	data.Reviews = addLinks(data.Reviews, snap.catalog, root).([]Review)
	history, e := s.taskHistory(snap)
	if e != nil {
		return webSnapshot{}, e
	}
	data.Resources, e = resourceReport(snap.catalog, snap.state, root)
	if e != nil {
		return webSnapshot{}, e
	}
	data.Workers, e = workerDetails(snap, history, now)
	if e != nil {
		return webSnapshot{}, e
	}
	data.Workers = addLinks(data.Workers, snap.catalog, root).(WorkerList)
	data.Todo = addLinks(data.Todo, snap.catalog, root).(Todo)
	stats, e := statistics(options{}, snap, history, now)
	stats = addLinks(stats, snap.catalog, root).(Statistics)
	return webSnapshot{data, stats, history, who}, e
}

// Each browser connection sees the latest complete snapshot. Polling is shared,
// serialized and canceled with the foreground process.
type webHub struct {
	mu      sync.RWMutex
	data    []byte
	version int
	changed chan struct{}
}

func (h *webHub) publish(v any) {
	b, _ := json.Marshal(v)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.data = b
	h.version++
	if h.changed != nil {
		close(h.changed)
	}
	h.changed = make(chan struct{})
}
func webHandler(ctx context.Context, root, who string, h *webHub) http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(webAssets, "webassets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		b, _ := webAssets.ReadFile("webassets/index.html")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unavailable", 500)
			return
		}
		last := -1
		for {
			h.mu.RLock()
			data, version, changed := h.data, h.version, h.changed
			h.mu.RUnlock()
			if version != last {
				rc := http.NewResponseController(w)
				_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, e := fmt.Fprintf(w, "data: %s\n\n", data); e != nil {
					return
				}
				flusher.Flush()
				last = version
			}
			select {
			case <-ctx.Done():
				return
			case <-r.Context().Done():
				return
			case <-changed:
			}
		}
	})
	mux.HandleFunc("/answer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST required", 405)
			return
		}
		var input struct {
			ID   int    `json:"id"`
			Text string `json:"text"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON required", 415)
			return
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if e := d.Decode(&input); e != nil || input.ID < 1 || strings.TrimSpace(input.Text) == "" {
			http.Error(w, "Question number and answer required", 400)
			return
		}
		if d.Decode(new(any)) != io.EOF {
			http.Error(w, "One JSON document required", 400)
			return
		}
		// Use the existing CLI operation, including identity, CAS, authorization and retries.
		out, e := executeBoardContext(ctx, options{command: "answer", args: []string{strconv.Itoa(input.ID), input.Text}, as: who, json: true}, root)
		w.Header().Set("Content-Type", "application/json")
		if e != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": e.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": out.result})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject cross-origin writes and DNS rebinding while keeping localhost unauthenticated.
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil || host != "127.0.0.1" {
			http.Error(w, "Loopback host required", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Same origin required", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
func runWeb(root string, o options) error {
	who, e := identity(o, root)
	if e != nil {
		return e
	}
	port := 0
	if o.port != "" {
		port, e = strconv.Atoi(o.port)
		if e != nil || port < 0 || port > 65535 {
			return fail(1, "port must be 0–65535")
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	first, e := readWeb(ctx, root, who)
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		return e
	}
	defer listener.Close()
	hub := &webHub{}
	hub.publish(map[string]any{"ok": true, "data": first})
	server := &http.Server{Handler: webHandler(ctx, root, who, hub), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	url := "http://" + listener.Addr().String()
	fmt.Println(url)
	if o.open {
		program, args := "xdg-open", []string{url}
		if runtime.GOOS == "darwin" {
			program = "open"
		}
		if runtime.GOOS == "windows" {
			program = "rundll32"
			args = []string{"url.dll,FileProtocolHandler", url}
		}
		if e := exec.CommandContext(ctx, program, args...).Run(); e != nil {
			fmt.Fprintln(os.Stderr, "Cannot open browser:", e)
		}
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				data, e := readWeb(ctx, root, who)
				if e != nil {
					hub.publish(map[string]any{"ok": false, "error": e.Error()})
				} else {
					hub.publish(map[string]any{"ok": true, "data": data})
				}
			}
		}
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	e = server.Serve(listener)
	cancel()
	<-finished
	<-shutdownDone
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
