// Package pump implements the bidirectional JSON-RPC proxy pipelines.
//
// Inbound handles the client → server direction, enforcing the Zero Trust
// policy engine (Phase 2).
//
// Outbound handles the server → client direction as a transparent passthrough.
package pump

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/aether-labs-studio/mcp-sentinel/internal/logx"
	"github.com/aether-labs-studio/mcp-sentinel/internal/policy"
)

// rpcEnvelope extracts the routing fields needed by the outbound pump.
type rpcEnvelope struct {
	Method string          `json:"method,omitempty"`
	ID     json.RawMessage `json:"id,omitempty"`
}

// newScanner returns a bufio.Scanner with a 10 MB per-line buffer.
// The large buffer is required for real-world MCP payloads that may contain
// full file contents, code context, or base64-encoded binary data.
func newScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	return s
}

// Inbound reads JSON-RPC lines from src (MCP client), enforces the Zero Trust
// policy engine, and forwards clean messages to serverW (MCP server subprocess).
//
// Policy violations are short-circuited: a JSON-RPC 2.0 error response is written
// back to clientW and the offending message is never forwarded to the server.
//
// The loop exits early if ctx is cancelled (e.g. signal received by the parent).
func Inbound(ctx context.Context, serverW io.Writer, clientW io.Writer, src io.Reader, engine *policy.Engine) {
	scanner := newScanner(src)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()
		result := engine.Enforce([]byte(line))

		if result.Violation != "" {
			log.Printf("[SENTINEL] 🚨 BLOCKED: %s in tools/call", result.Violation)
			errResp, err := policy.BuildErrorResponse(result.ID)
			if err != nil {
				log.Printf("sentinel: failed to build block response: %v", err)
				continue
			}
			if _, err := fmt.Fprintln(clientW, string(errResp)); err != nil {
				log.Printf("sentinel: error writing block response: %v", err)
			}
			continue
		}

		switch {
		case result.Method != "":
			logx.Debugf("[SENTINEL] 🟢 INBOUND: %s", result.Method)
		case len(result.ID) > 0 && string(result.ID) != "null":
			logx.Debugf("[SENTINEL] 🟢 INBOUND: Response ID %s", string(result.ID))
		}

		if _, err := fmt.Fprintln(serverW, line); err != nil {
			log.Printf("sentinel: inbound write error: %v", err)
			return
		}

	}

	if err := scanner.Err(); err != nil {
		log.Printf("sentinel: inbound scanner error: %v", err)
	}
}

// Outbound reads JSON-RPC lines from src (MCP server subprocess) and forwards
// them to dst (MCP client) without mutating the payload.
// The loop exits early if ctx is cancelled (e.g. signal received by the parent).
func Outbound(ctx context.Context, dst io.Writer, src io.Reader) {
	scanner := newScanner(src)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()
		raw := []byte(line)

		var env rpcEnvelope
		if err := json.Unmarshal(raw, &env); err == nil {
			switch {
			case env.Method != "":
				logx.Debugf("[SENTINEL] 🔵 OUTBOUND: %s", env.Method)
			case len(env.ID) > 0 && string(env.ID) != "null":
				logx.Debugf("[SENTINEL] 🔵 OUTBOUND: Response ID %s", string(env.ID))
			}
		}

		if _, err := fmt.Fprintln(dst, line); err != nil {
			log.Printf("sentinel: outbound write error: %v", err)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("sentinel: outbound scanner error: %v", err)
	}
}
