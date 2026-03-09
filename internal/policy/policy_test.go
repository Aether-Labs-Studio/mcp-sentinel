package policy

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/aether-labs-studio/mcp-sentinel/internal/config"
)

func testEngine() *Engine {
	rules := []config.CompiledRule{
		{Pattern: regexp.MustCompile(`(?i)/etc/passwd`), Description: "Sensitive file access (/etc/passwd)"},
		{Pattern: regexp.MustCompile(`(?i)/etc/shadow`), Description: "Sensitive file access (/etc/shadow)"},
		{Pattern: regexp.MustCompile(`(?i)\.ssh`), Description: "SSH credential access (.ssh)"},
		{Pattern: regexp.MustCompile(`(?i)\.env`), Description: "Environment secrets access (.env)"},
		{Pattern: regexp.MustCompile(`(?i)/proc/self`), Description: "Proc filesystem access (/proc/self)"},
		{Pattern: regexp.MustCompile(`\.\.[/\\]`), Description: "Path Traversal (..)"},
	}
	return NewEngine(rules, nil, nil)
}

func testEngineWithBlockedTools(tools []string) *Engine {
	return NewEngine(nil, tools, nil)
}

func TestEnforce_PassesCleanToolsCall(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/home/user/docs/report.txt"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation != "" {
		t.Errorf("expected no violation, got: %s", result.Violation)
	}
	if result.Method != "tools/call" {
		t.Errorf("expected method tools/call, got: %s", result.Method)
	}
}

func TestEnforce_BlocksEtcPasswd(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/etc/passwd"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for /etc/passwd, got none")
	}
}

func TestEnforce_BlocksEtcShadow(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/etc/shadow"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for /etc/shadow, got none")
	}
}

func TestEnforce_BlocksSSHKey(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/home/user/.ssh/id_rsa"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for .ssh path, got none")
	}
}

func TestEnforce_BlocksDotEnv(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/app/.env"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for .env, got none")
	}
}

func TestEnforce_BlocksPathTraversal(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"../../etc/shadow"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for path traversal, got none")
	}
}

func TestEnforce_PassesNonToolsCall(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":7,"method":"resources/list","params":{}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation != "" {
		t.Errorf("expected no violation for resources/list, got: %s", result.Violation)
	}
}

func TestEnforce_PassesUnparseable(t *testing.T) {
	result := testEngine().Enforce([]byte(`not valid json`))
	if result.Violation != "" {
		t.Errorf("expected no violation for unparseable input, got: %s", result.Violation)
	}
}

func TestEnforce_BlockedResultPreservesID(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/etc/passwd"}}}`
	result := testEngine().Enforce([]byte(line))
	if result.Violation == "" {
		t.Fatal("expected violation")
	}
	var id float64
	if err := json.Unmarshal(result.ID, &id); err != nil || id != 42 {
		t.Errorf("expected ID 42, got %s", string(result.ID))
	}
}

func TestEnforce_BlocksToolByName(t *testing.T) {
	engine := testEngineWithBlockedTools([]string{"delete_file", "execute_code"})
	line := `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"/tmp/safe.txt"}}}`
	result := engine.Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for blocked tool name, got none")
	}
	if !strings.Contains(result.Violation, "delete_file") {
		t.Errorf("expected violation to mention tool name, got: %s", result.Violation)
	}
}

func TestEnforce_AllowsUnblockedTool(t *testing.T) {
	engine := testEngineWithBlockedTools([]string{"delete_file"})
	line := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/tmp/safe.txt"}}}`
	result := engine.Enforce([]byte(line))
	if result.Violation != "" {
		t.Errorf("expected no violation for allowed tool, got: %s", result.Violation)
	}
}

func TestEnforce_BlockedToolTakesPrecedenceOverSafeArgs(t *testing.T) {
	engine := testEngineWithBlockedTools([]string{"safe_looking_tool"})
	line := `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"safe_looking_tool","arguments":{"path":"/tmp/totally-safe.txt"}}}`
	result := engine.Enforce([]byte(line))
	if result.Violation == "" {
		t.Error("expected violation for blocked tool regardless of safe args, got none")
	}
}

func TestBuildErrorResponse_ContainsExpectedFields(t *testing.T) {
	id := json.RawMessage(`99`)
	resp, err := BuildErrorResponse(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(resp)
	if !strings.Contains(s, `"jsonrpc":"2.0"`) {
		t.Error("missing jsonrpc field")
	}
	if !strings.Contains(s, `"id":99`) {
		t.Error("missing or wrong id field")
	}
	if !strings.Contains(s, `"code":-32600`) {
		t.Error("missing error code")
	}
	if !strings.Contains(s, "Policy Violation") {
		t.Error("missing policy violation message")
	}
}
