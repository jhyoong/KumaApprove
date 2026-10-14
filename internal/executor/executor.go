package executor

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
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

	// maxTimeoutSeconds caps the per-call --timeout override.
	maxTimeoutSeconds = 3600

	// maxPrivilegedCmdLen keeps a root command short enough to be shown in
	// full in the approval message; it must never be approved truncated.
	maxPrivilegedCmdLen = 3000
)

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
	DenyListPatterns []string
	// Privileged is set when the process runs as root. Commands then get a
	// root-appropriate environment.
	Privileged bool
}

// TimeoutError indicates that a command exceeded its allowed execution time.
type TimeoutError struct {
	TimeoutSeconds int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command timeout after %d seconds", e.TimeoutSeconds)
}

// DeniedError indicates that a command matched the deny list.
type DeniedError struct {
	Cmd string
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("command denied: %q matches deny list", e.Cmd)
}

// ExecResult holds the output of a command execution.
type ExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
}

// ExecService implements the service.Service interface for running commands.
type ExecService struct {
	config       ExecConfig
	denyPatterns []*regexp.Regexp
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
				{Name: "timeout", Required: false, Description: fmt.Sprintf("Timeout in seconds (max %d)", maxTimeoutSeconds)},
			},
		},
	}
}

// Execute runs the given action. Only the "run" action is supported.
func (e *ExecService) Execute(action string, args map[string]string) (*service.Result, error) {
	if err := e.Validate(action, args); err != nil {
		return nil, err
	}

	cmdStr := args["cmd"]
	shellMode := strings.ToLower(args["shell"]) == "true"

	timeoutSeconds, err := e.timeoutSeconds(args)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
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
	if e.config.Privileged {
		cmd.Env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"LANG=en_US.UTF-8",
		}
	}

	if e.config.WorkingDirectory != "" {
		cmd.Dir = e.config.WorkingDirectory
	}

	stdoutBuf := newLimitWriter(e.config.MaxOutputBytes)
	stderrBuf := newLimitWriter(e.config.MaxOutputBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &TimeoutError{TimeoutSeconds: timeoutSeconds}
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

// Validate checks a request before it reaches the approval gate: unknown
// parameters, a bad timeout, or a command matching the deny list are all
// refused without asking for approval. Deny-list matches return *DeniedError.
func (e *ExecService) Validate(action string, args map[string]string) error {
	if action != "run" {
		return fmt.Errorf("unknown action: %s", action)
	}

	var unknown []string
	for k := range args {
		if k != "cmd" && k != "shell" && k != "timeout" {
			unknown = append(unknown, "--"+k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown parameter(s): %s", strings.Join(unknown, ", "))
	}

	cmdStr := args["cmd"]
	if strings.TrimSpace(cmdStr) == "" {
		return fmt.Errorf("missing required parameter: cmd")
	}
	if e.config.Privileged && len(cmdStr) > maxPrivilegedCmdLen {
		return fmt.Errorf("command is %d bytes; commands run as root are limited to %d so the approval message can show them in full", len(cmdStr), maxPrivilegedCmdLen)
	}
	if _, err := e.timeoutSeconds(args); err != nil {
		return err
	}
	if e.isDenied(cmdStr) {
		return &DeniedError{Cmd: cmdStr}
	}
	return nil
}

// timeoutSeconds returns the --timeout override if given, else the
// configured default.
func (e *ExecService) timeoutSeconds(args map[string]string) (int, error) {
	raw, ok := args["timeout"]
	if !ok {
		return e.config.TimeoutSeconds, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxTimeoutSeconds {
		return 0, fmt.Errorf("invalid timeout %q: must be a whole number of seconds between 1 and %d", raw, maxTimeoutSeconds)
	}
	return n, nil
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
