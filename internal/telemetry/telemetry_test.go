package telemetry

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registerClient(h *Hub, ch chan Event) func() {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}
}

func TestParseHubMode(t *testing.T) {
	tests := []struct {
		in      string
		want    HubMode
		wantErr bool
	}{
		{"", HubModeRelay, false},
		{"relay", HubModeRelay, false},
		{"on_update", HubModeOnUpdate, false},
		{"always_takeover", HubModeAlwaysTakeover, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		got, err := ParseHubMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParseHubMode(%q) expected error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseHubMode(%q) unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ParseHubMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNoOpEmitter_DoesNothing(t *testing.T) {
	NoOpEmitter{}.Broadcast(Event{Type: "INVOCATION", Tool: "t"})
}

func TestBroadcast_CompletesWithoutDelay(t *testing.T) {
	h := NewHub()
	defer h.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			h.Broadcast(Event{Type: "INVOCATION", Tool: "test_tool"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("Broadcast blocked — possible deadlock or blocking call")
	}
}

func TestBroadcast_SetsTimestamp(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := make(chan Event, 1)
	defer registerClient(h, ch)()

	before := time.Now().UTC()
	h.Broadcast(Event{Type: "BLOCK", Tool: "tool", Reason: "test"})

	select {
	case e := <-ch:
		if e.Timestamp == "" {
			t.Fatal("expected non-empty timestamp")
		}
		ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			t.Fatalf("timestamp has invalid format: %v", err)
		}
		if ts.Before(before) {
			t.Error("timestamp is before the Broadcast call")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("event not received in time")
	}
}

func TestBroadcast_PreservesExistingTimestamp(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := make(chan Event, 1)
	defer registerClient(h, ch)()

	const fixedTS = "2026-01-01T00:00:00.000000000Z"
	h.Broadcast(Event{Type: "INVOCATION", Tool: "t", Timestamp: fixedTS})

	select {
	case e := <-ch:
		if e.Timestamp != fixedTS {
			t.Errorf("timestamp overwritten: got %q, want %q", e.Timestamp, fixedTS)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("event not received")
	}
}

func TestHub_EventsReachConnectedClient(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := make(chan Event, 4)
	defer registerClient(h, ch)()

	h.Broadcast(Event{Type: "BLOCK", Tool: "dangerous_tool", Reason: "test reason"})

	select {
	case e := <-ch:
		if e.Type != "BLOCK" || e.Tool != "dangerous_tool" || e.Reason != "test reason" {
			t.Errorf("unexpected event: %+v", e)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("event not received in time")
	}
}

func TestHub_SlowClientDoesNotBlockFastClient(t *testing.T) {
	h := NewHub()
	defer h.Close()

	slowCh := make(chan Event)
	defer registerClient(h, slowCh)()
	fastCh := make(chan Event, 4)
	defer registerClient(h, fastCh)()

	h.Broadcast(Event{Type: "INVOCATION", Tool: "tool"})

	select {
	case e := <-fastCh:
		if e.Type != "INVOCATION" {
			t.Errorf("unexpected event type: %s", e.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("fast client did not receive event — slow client may be blocking dispatch")
	}
}

func TestBroadcast_NoClients_DoesNotBlock(t *testing.T) {
	h := NewHub()
	defer h.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Broadcast(Event{Type: "INVOCATION"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("Broadcast blocked with no registered clients")
	}
}

func TestHub_AuditLog_WritesJSONL(t *testing.T) {
	h := NewHub()

	var captured []byte
	hubClosed := make(chan struct{})
	h.auditLog = writerFunc(func(p []byte) (int, error) {
		captured = append(captured, p...)
		return len(p), nil
	})
	h.closeAudit = func() { close(hubClosed) }

	ch := make(chan Event, 1)
	defer registerClient(h, ch)()

	h.Broadcast(Event{Type: "BLOCK", Tool: "bad_tool", Reason: "test reason"})

	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("event not received by client in time")
	}

	h.Close()
	select {
	case <-hubClosed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hub did not close in time")
	}

	raw := strings.TrimSpace(string(captured))
	if raw == "" {
		t.Fatal("audit log is empty — no line was written")
	}
	lines := strings.Split(raw, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d: %q", len(lines), raw)
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("audit line is not valid JSON: %v — line: %q", err, lines[0])
	}
	if e.Type != "BLOCK" || e.Tool != "bad_tool" || e.Reason != "test reason" {
		t.Errorf("unexpected event in audit: %+v", e)
	}
	if e.Timestamp == "" {
		t.Error("event in audit must have a timestamp")
	}
}

func TestServeHTTP_StreamsEvents(t *testing.T) {
	h := NewHub()
	defer h.Close()

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan string, 1)
	go func() {
		h.ServeHTTP(w, req)
		done <- w.Body.String()
	}()

	time.Sleep(20 * time.Millisecond)
	h.Broadcast(Event{Type: "INVOCATION", Tool: "tool_a"})
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case body := <-done:
		if !strings.Contains(body, "tool_a") {
			t.Fatalf("expected SSE payload to contain event, got %q", body)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ServeHTTP did not terminate after request cancellation")
	}
}

func TestStartOrRelay_ReturnsHubWhenPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not obtain free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	emitter := StartOrRelay(addr, "", HubModeRelay)
	if h, ok := emitter.(*Hub); ok {
		defer h.Close()
	} else {
		t.Errorf("expected *Hub when port is free, got %T", emitter)
	}
}

func TestStartOrRelay_ReturnsNoOpWhenPortOccupied(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not open listener: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	emitter := StartOrRelay(addr, "", HubModeRelay)
	if _, ok := emitter.(NoOpEmitter); !ok {
		t.Errorf("expected NoOpEmitter when port is occupied, got %T", emitter)
	}
}

func TestStartOrRelay_EndToEnd_SecondInstanceSilent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not obtain free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	first := StartOrRelay(addr, "", HubModeRelay)
	hub, ok := first.(*Hub)
	if !ok {
		t.Fatalf("first StartOrRelay should return *Hub, got %T", first)
	}
	defer hub.Close()

	ch := make(chan Event, 2)
	defer registerClient(hub, ch)()

	second := StartOrRelay(addr, "", HubModeRelay)
	if _, ok := second.(NoOpEmitter); !ok {
		t.Fatalf("second StartOrRelay should return NoOpEmitter, got %T", second)
	}

	first.Broadcast(Event{Type: "INVOCATION", Tool: "tool_from_hub"})
	second.Broadcast(Event{Type: "BLOCK", Tool: "tool_from_noop"})

	select {
	case e := <-ch:
		if e.Tool != "tool_from_hub" {
			t.Fatalf("unexpected event from hub stream: %+v", e)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive event from primary hub")
	}

	select {
	case e := <-ch:
		t.Fatalf("unexpected event from second process: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRotatingWriter_WritesData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	rw, err := newRotatingWriter(path)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer rw.Close()

	data := []byte("hello world\n")
	n, err := rw.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	rw.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestRotatingWriter_RotatesWhenSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	rw, err := newRotatingWriter(path)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}

	rw.written = auditMaxBytes - 5
	trigger := []byte("123456\n")
	if _, err := rw.Write(trigger); err != nil {
		t.Fatalf("Write (trigger rotation): %v", err)
	}
	rw.Close()

	backup := path + ".1"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Error("expected backup file after rotation, but it does not exist")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile (active log): %v", err)
	}
	if string(got) != string(trigger) {
		t.Errorf("expected active log to contain only %q after rotation, got %q", trigger, got)
	}
}

func TestRotatingWriter_ExistingFileSizeAccounted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	existing := make([]byte, auditMaxBytes)
	if err := os.WriteFile(path, existing, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rw, err := newRotatingWriter(path)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}

	if _, err := rw.Write([]byte("x\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rw.Close()

	backup := path + ".1"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Error("expected backup file when pre-existing file was at threshold")
	}
}
