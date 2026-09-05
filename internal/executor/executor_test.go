package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhyoong/KumaApprove/internal/approval"
)

func TestDirectExec(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Execute("run", map[string]string{
		"cmd": "echo hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := result.Data.(*ExecResult)
	if out.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", out.ExitCode)
	}
	if strings.TrimSpace(out.Stdout) != "hello world" {
		t.Errorf("expected stdout 'hello world', got %q", out.Stdout)
	}
}

func TestShellMode(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Execute("run", map[string]string{
		"cmd":   "echo foo && echo bar",
		"shell": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := result.Data.(*ExecResult)
	if out.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", out.ExitCode)
	}
	stdout := strings.TrimSpace(out.Stdout)
	if stdout != "foo\nbar" {
		t.Errorf("expected stdout 'foo\\nbar', got %q", stdout)
	}
}

func TestDenyListBlock(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		cmd  string
	}{
		{"sudo", "sudo apt-get install something"},
		{"rm -rf", "rm -rf /"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Execute("run", map[string]string{
				"cmd": tc.cmd,
			})
			if err == nil {
				t.Errorf("expected error for denied command %q, got nil", tc.cmd)
			}
			if !strings.Contains(err.Error(), "denied") {
				t.Errorf("expected error to contain 'denied', got %q", err.Error())
			}
		})
	}
}

func TestSafeListClassification(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		cmd  string
		tier string
	}{
		{"echo hello", approval.TierAuto},
		{"ls -la", approval.TierAuto},
		{"git status", approval.TierApprove},
	}

	for _, tc := range tests {
		t.Run(tc.cmd, func(t *testing.T) {
			tier := svc.GetTier(tc.cmd)
			if tier != tc.tier {
				t.Errorf("GetTier(%q) = %q, want %q", tc.cmd, tier, tc.tier)
			}
		})
	}
}

func TestShellBumpsToApprove(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Even a safe "echo" command should be bumped to "approve" in shell mode.
	tier := svc.GetTierWithShell("echo hello")
	if tier != approval.TierApprove {
		t.Errorf("GetTierWithShell('echo hello') = %q, want %q", tier, approval.TierApprove)
	}
}

func TestTimeout(t *testing.T) {
	svc, err := New(ExecConfig{
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Execute("run", map[string]string{
		"cmd": "sleep 10",
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "killed") {
		t.Errorf("expected timeout-related error, got %q", err.Error())
	}
}

func TestOutputTruncation(t *testing.T) {
	svc, err := New(ExecConfig{
		MaxOutputBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Generate output longer than 10 bytes.
	result, err := svc.Execute("run", map[string]string{
		"cmd": "echo abcdefghijklmnopqrstuvwxyz",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := result.Data.(*ExecResult)
	if !out.Truncated {
		t.Error("expected Truncated to be true")
	}
	if len(out.Stdout) > 10 {
		t.Errorf("expected stdout length <= 10, got %d", len(out.Stdout))
	}
}

func TestMissingCmd(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Execute("run", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing cmd, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter") {
		t.Errorf("expected 'missing required parameter' error, got %q", err.Error())
	}
}

func TestUnknownAction(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Execute("deploy", map[string]string{
		"cmd": "echo hello",
	})
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected 'unknown action' error, got %q", err.Error())
	}
}

func TestActions(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	actions := svc.Actions()
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Name != "run" {
		t.Errorf("expected action name 'run', got %q", actions[0].Name)
	}
}

func TestExecResultJSONKeys(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Execute("run", map[string]string{
		"cmd": "echo hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	s := string(b)

	if strings.Contains(s, "Stdout") {
		t.Errorf("JSON contains capitalized 'Stdout', expected 'stdout': %s", s)
	}
	if strings.Contains(s, "ExitCode") {
		t.Errorf("JSON contains capitalized 'ExitCode', expected 'exit_code': %s", s)
	}
	if !strings.Contains(s, `"stdout"`) {
		t.Errorf("JSON missing 'stdout' key: %s", s)
	}
	if !strings.Contains(s, `"exit_code"`) {
		t.Errorf("JSON missing 'exit_code' key: %s", s)
	}
}

func TestName(t *testing.T) {
	svc, err := New(ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Name() != "exec" {
		t.Errorf("expected name 'exec', got %q", svc.Name())
	}
}
