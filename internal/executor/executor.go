package executor

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/service"
)

// limitWriter is a bounded writer that stops buffering once max bytes is reached.
type limitWriter struct {
	buf       []byte
	n         int
	max       int
	truncated bool
}

func newLimitWriter(max int) *limitWriter {
	return &limitWriter{buf: make([]byte, max), max: max}
}

func (w *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	avail := w.max - w.n
	if avail <= 0 {
		w.truncated = true
		return n, nil
	}
	if len(p) > avail {
		w.truncated = true
		p = p[:avail]
	}
	copy(w.buf[w.n:], p)
	w.n += len(p)
	return n, nil
}

func (w *limitWriter) String() string {
	return string(w.buf[:w.n])
}

// Default limits.
const (
	defaultTimeoutSeconds = 30
	defaultMaxOutputBytes = 1048576 // 1 MB
)

// Default safe list: command name -> approval tier.
var defaultSafeList = map[string]string{
	"echo":   approval.TierAuto,
	"ls":     approval.TierAuto,
	"cat":    approval.TierAuto,
	"head":   approval.TierAuto,
	"tail":   approval.TierAuto,
	"wc":     approval.TierAuto,
	"grep":   approval.TierAuto,
	"find":   approval.TierAuto,
	"date":   approval.TierAuto,
	"whoami": approval.TierAuto,
	"pwd":    approval.TierAuto,
	"uname":  approval.TierAuto,
	"which":  approval.TierAuto,
	"env":    approval.TierApprove,
	"git":    approval.TierApprove,
}

// Default deny patterns.
var defaultDenyPatterns = []string{
	`\bsudo\b`,
	`\brm\s+(-[a-zA-Z]*f|-rf|--force)`,
	`\bmkfs\b`,
	`\bdd\b`,
	`\bfdisk\b`,
	`\bchmod\b`,
	`\bchown\b`,
}

// ExecConfig holds configuration for the command executor.
type ExecConfig struct {
	WorkingDirectory string
	TimeoutSeconds   int
	MaxOutputBytes   int
	SafeList         map[string]string
	DenyListPatterns []string
}

// TimeoutError indicates that a command exceeded its allowed execution time.
type TimeoutError struct {
	TimeoutSeconds int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command timeout after %d seconds", e.TimeoutSeconds)
}

// ExecResult holds the output of a command execution.
type ExecResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

// ExecService implements the service.Service interface for running commands.
type ExecService struct {
	config       ExecConfig
	denyPatterns []*regexp.Regexp
	safeList     map[string]string
}

// New creates a new ExecService from the given configuration.
// It compiles deny-list regex patterns and applies defaults where needed.
// Returns an error if any deny pattern is an invalid regular expression.
func New(cfg ExecConfig) (*ExecService, error) {
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = defaultTimeoutSeconds
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}

	safeList := cfg.SafeList
	if safeList == nil {
		safeList = defaultSafeList
	}

	denyRaw := cfg.DenyListPatterns
	if denyRaw == nil {
		denyRaw = defaultDenyPatterns
	}

	compiled := make([]*regexp.Regexp, 0, len(denyRaw))
	for _, p := range denyRaw {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid deny pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}

	return &ExecService{
		config:       cfg,
		denyPatterns: compiled,
		safeList:     safeList,
	}, nil
}

// Name returns the service name.
func (e *ExecService) Name() string { return "exec" }

// Actions returns the action definitions for the executor service.
func (e *ExecService) Actions() []service.ActionDefinition {
	return []service.ActionDefinition{
		{
			Name:        "run",
			DefaultTier: approval.TierApprove,
			Description: "Run a command",
			Params: []service.ParamDef{
				{Name: "cmd", Required: true, Description: "Command to execute"},
				{Name: "shell", Required: false, Description: "Use shell mode (true/false)"},
			},
		},
	}
}

// Execute runs the given action. Only the "run" action is supported.
func (e *ExecService) Execute(action string, args map[string]string) (*service.Result, error) {
	if action != "run" {
		return nil, fmt.Errorf("unknown action: %s", action)
	}

	cmdStr, ok := args["cmd"]
	if !ok || strings.TrimSpace(cmdStr) == "" {
		return nil, fmt.Errorf("missing required parameter: cmd")
	}

	if e.isDenied(cmdStr) {
		return nil, fmt.Errorf("command denied: %q matches deny list", cmdStr)
	}

	shellMode := strings.ToLower(args["shell"]) == "true"

	timeout := time.Duration(e.config.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if shellMode {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
	} else {
		parts := strings.Fields(cmdStr)
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	}

	// Restricted environment.
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"LANG=en_US.UTF-8",
	}

	if e.config.WorkingDirectory != "" {
		cmd.Dir = e.config.WorkingDirectory
	}

	stdoutBuf := newLimitWriter(e.config.MaxOutputBytes)
	stderrBuf := newLimitWriter(e.config.MaxOutputBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &TimeoutError{TimeoutSeconds: e.config.TimeoutSeconds}
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("command execution failed: %w", err)
		}
	}

	return &service.Result{
		Data: &ExecResult{
			Stdout:    stdoutBuf.String(),
			Stderr:    stderrBuf.String(),
			ExitCode:  exitCode,
			Truncated: stdoutBuf.truncated || stderrBuf.truncated,
		},
	}, nil
}

// isDenied checks if the command matches any deny-list pattern.
func (e *ExecService) isDenied(cmd string) bool {
	for _, re := range e.denyPatterns {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// GetTier returns the approval tier for a command based on the safe list.
// Returns "approve" if the command is not in the safe list.
// Returns "deny" if the command matches the deny list.
func (e *ExecService) GetTier(cmd string) string {
	if e.isDenied(cmd) {
		return approval.TierDeny
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return approval.TierApprove
	}

	cmdName := parts[0]
	if tier, ok := e.safeList[cmdName]; ok {
		return tier
	}
	return approval.TierApprove
}

// GetTierWithShell returns the approval tier for a command in shell mode.
// Shell mode always returns "approve" regardless of the safe list.
func (e *ExecService) GetTierWithShell(cmd string) string {
	if e.isDenied(cmd) {
		return approval.TierDeny
	}
	return approval.TierApprove
}
