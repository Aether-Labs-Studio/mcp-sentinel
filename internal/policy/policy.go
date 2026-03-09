package policy

import (
	"encoding/json"
	"fmt"

	"github.com/aether-labs-studio/mcp-sentinel/internal/config"
	"github.com/aether-labs-studio/mcp-sentinel/internal/telemetry"
)

// inboundEnvelope is a strict partial unmarshal for inbound messages (client → server).
// Includes Params for inspection on tools/call requests.
type inboundEnvelope struct {
	Method string          `json:"method,omitempty"`
	ID     json.RawMessage `json:"id,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// toolsCallParams extracts the required fields from a tools/call request body.
// Only Name and Arguments are captured; the rest of the payload is discarded.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Result holds the outcome of analyzing an inbound line.
// An empty Violation means the request is safe to forward.
type Result struct {
	Method    string
	ID        json.RawMessage
	Violation string
}

// Engine applies the Zero Trust policy to inbound messages.
// It holds compiled rules and an optional telemetry emitter — O(1) per evaluation at runtime.
// A nil emitter is valid; telemetry is simply skipped.
type Engine struct {
	rules        []config.CompiledRule
	blockedTools map[string]struct{} // O(1) lookup; tool names blocked unconditionally
	emitter      telemetry.Emitter
}

// NewEngine creates an Engine with the supplied rules and telemetry emitter.
// blockedTools is the list of tool names blocked unconditionally (regardless of arguments).
// emitter may be nil; no telemetry is emitted in that case.
func NewEngine(rules []config.CompiledRule, blockedTools []string, emitter telemetry.Emitter) *Engine {
	bt := make(map[string]struct{}, len(blockedTools))
	for _, t := range blockedTools {
		bt[t] = struct{}{}
	}
	return &Engine{rules: rules, blockedTools: bt, emitter: emitter}
}

// Enforce analyzes a raw JSON-RPC line against the Zero Trust policy.
// Only tools/call messages are inspected at the params level; all others pass through.
// Emits an INVOCATION or BLOCK telemetry event if an emitter is configured.
func (e *Engine) Enforce(line []byte) Result {
	var env inboundEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Result{}
	}

	result := Result{Method: env.Method, ID: env.ID}

	if env.Method != "tools/call" || len(env.Params) == 0 {
		return result
	}

	var params toolsCallParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return result // malformed params → let the server handle it
	}

	// Blocked-tool check: O(1) map lookup, evaluated before regex scanning.
	if _, blocked := e.blockedTools[params.Name]; blocked {
		result.Violation = fmt.Sprintf("Tool '%s' is explicitly blocked by policy", params.Name)
		if e.emitter != nil {
			e.emitter.Broadcast(telemetry.Event{
				Type:   "BLOCK",
				Tool:   params.Name,
				Reason: result.Violation,
			})
		}
		return result
	}

	rawArgs := string(params.Arguments)
	for _, rule := range e.rules {
		if rule.Pattern.MatchString(rawArgs) {
			result.Violation = rule.Description
			if e.emitter != nil {
				e.emitter.Broadcast(telemetry.Event{
					Type:   "BLOCK",
					Tool:   params.Name,
					Reason: result.Violation,
				})
			}
			return result
		}
	}

	// Clean tools/call — emit invocation event.
	if e.emitter != nil {
		e.emitter.Broadcast(telemetry.Event{
			Type: "INVOCATION",
			Tool: params.Name,
		})
	}
	return result
}

// errCodePolicyViolation is the JSON-RPC 2.0 error code returned when Sentinel
// blocks a request. -32600 is "Invalid Request" per the JSON-RPC 2.0 specification,
// chosen because a policy-violating request is semantically invalid from the
// server's perspective even if it is syntactically well-formed.
const errCodePolicyViolation = -32600

// rpcErrorBody is the error object per the JSON-RPC 2.0 specification.
type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcErrorResponse is the response Sentinel injects directly to the client
// when it intercepts a request that violates policy.
type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcErrorBody    `json:"error"`
}

// BuildErrorResponse constructs a serialized JSON-RPC 2.0 error response.
// Returns an error if serialization fails (not expected under normal conditions).
func BuildErrorResponse(id json.RawMessage) ([]byte, error) {
	resp := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcErrorBody{Code: errCodePolicyViolation, Message: "Invalid Request - Policy Violation"},
	}
	return json.Marshal(resp)
}
