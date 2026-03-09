package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aether-labs-studio/mcp-sentinel/internal/logx"
)

// Event is the telemetry event emitted by Sentinel components.
type Event struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"` // "INVOCATION" or "BLOCK"
	Tool      string `json:"tool"`
	Reason    string `json:"reason,omitempty"` // only present on BLOCK events
}

// Emitter is the minimal interface for fire-and-forget telemetry emission.
// A nil Emitter is valid — callers check for nil before calling.
type Emitter interface {
	Broadcast(e Event)
}

// NoOpEmitter silently drops all events.
type NoOpEmitter struct{}

// Broadcast implements Emitter.
func (NoOpEmitter) Broadcast(Event) {}

// HubMode remains accepted for CLI/config compatibility in CE.
type HubMode string

const (
	HubModeRelay          HubMode = "relay"
	HubModeOnUpdate       HubMode = "on_update"
	HubModeAlwaysTakeover HubMode = "always_takeover"
	DefaultHubMode        HubMode = HubModeRelay
)

// ParseHubMode validates and normalizes a hub mode string.
// Empty string resolves to DefaultHubMode.
func ParseHubMode(raw string) (HubMode, error) {
	mode := HubMode(strings.TrimSpace(strings.ToLower(raw)))
	if mode == "" {
		return DefaultHubMode, nil
	}
	switch mode {
	case HubModeRelay, HubModeOnUpdate, HubModeAlwaysTakeover:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid telemetry hub mode %q (allowed: relay, on_update, always_takeover)", raw)
	}
}

// Hub manages active SSE connections and dispatches events to all subscribers.
// Broadcast never blocks: if the internal buffer is full, the event is dropped.
type Hub struct {
	events     chan Event
	quit       chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex
	clients    map[chan Event]struct{}
	auditLog   io.Writer
	closeAudit func()
}

// NewHub creates a Hub with a 64-event buffer and starts the dispatch loop.
func NewHub() *Hub {
	h := &Hub{
		events:  make(chan Event, 64),
		quit:    make(chan struct{}),
		clients: make(map[chan Event]struct{}),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	defer func() {
		if h.closeAudit != nil {
			h.closeAudit()
		}
	}()

	for {
		select {
		case e := <-h.events:
			if h.auditLog != nil {
				if line, err := json.Marshal(e); err == nil {
					if _, err := h.auditLog.Write(append(line, '\n')); err != nil {
						log.Printf("sentinel: audit log write error: %v", err)
					}
				}
			}

			h.mu.Lock()
			for ch := range h.clients {
				select {
				case ch <- e:
				default:
				}
			}
			h.mu.Unlock()
		case <-h.quit:
			return
		}
	}
}

// Broadcast implements Emitter. Never blocks.
func (h *Hub) Broadcast(e Event) {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	select {
	case h.events <- e:
	default:
		logx.Debugf("sentinel: telemetry buffer full — event dropped (type=%s tool=%s)", e.Type, e.Tool)
	}
}

// Close shuts down the dispatch loop. Safe to call multiple times.
func (h *Hub) Close() {
	h.closeOnce.Do(func() { close(h.quit) })
}

const auditMaxBytes = 10 * 1024 * 1024 // 10 MiB

type rotatingWriter struct {
	path    string
	file    *os.File
	written int64
}

func newRotatingWriter(path string) (*rotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("audit: failed to create directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	var size int64
	if info != nil {
		size = info.Size()
	}
	return &rotatingWriter{path: path, file: f, written: size}, nil
}

func (rw *rotatingWriter) Write(p []byte) (int, error) {
	if rw.written+int64(len(p)) > auditMaxBytes {
		if err := rw.rotate(); err != nil {
			log.Printf("sentinel: audit log rotation failed: %v", err)
		}
	}
	n, err := rw.file.Write(p)
	rw.written += int64(n)
	return n, err
}

func (rw *rotatingWriter) rotate() error {
	backup := rw.path + ".1"
	rw.file.Close()
	if err := os.Rename(rw.path, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("audit: rename to backup failed: %w", err)
	}
	f, err := os.OpenFile(rw.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("audit: failed to open new log file: %w", err)
	}
	rw.file = f
	rw.written = 0
	logx.Debugf("sentinel: audit log rotated → backup at %s", backup)
	return nil
}

func (rw *rotatingWriter) Close() error { return rw.file.Close() }

func openAuditFile(path string) (*rotatingWriter, error) { return newRotatingWriter(path) }

func startHub(ln net.Listener, addr, auditPath string) Emitter {
	hub := NewHub()

	if auditPath != "" {
		rw, err := openAuditFile(auditPath)
		if err != nil {
			logx.Debugf("sentinel: audit log unavailable at %q: %v (continuing without it)", auditPath, err)
		} else {
			hub.auditLog = rw
			hub.closeAudit = func() { _ = rw.Close() }
			logx.Debugf("sentinel: audit log → %s (max %d MiB, 1 backup)", auditPath, auditMaxBytes/1024/1024)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(frontendHTML)
	})
	mux.Handle("/events", hub)

	go func() {
		logx.Debugf("sentinel: telemetry SSE hub at http://%s/events", addr)
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("sentinel: telemetry server error: %v", err)
		}
	}()

	return hub
}

// StartOrRelay starts the local SSE hub when possible.
// If another Sentinel instance already owns the port, CE degrades to a silent NoOpEmitter.
func StartOrRelay(addr, auditPath string, _ HubMode) Emitter {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logx.Debugf("sentinel: telemetry port %s already in use — CE running silently in this process", addr)
		return NoOpEmitter{}
	}
	return startHub(ln, addr, auditPath)
}

// ServeHTTP implements http.Handler for the GET /events SSE endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan Event, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	for {
		select {
		case e := <-ch:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
