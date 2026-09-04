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
	"env":    approval.TierAuto,
	"git":    approval.TierApprove,
}

// Default deny patterns.
var defaultDenyPatterns = []string{
	`^sudo`,
	`\brm\s+(-[a-zA-Z]*f|-rf|--force)`,
	`\bmkfs\b`,
	`\bdd\b`,
	`\bformat\b`,
	`\bfdisk\b`,
	`\bchmod\s+777`,
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
func New(cfg ExecConfig) *ExecService {
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
		compiled = append(compiled, regexp.MustCompile(p))
	}

	return &ExecService{
		config:       cfg,
		denyPatterns: compiled,
		safeList:     safeList,
	}
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

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timeout after %d seconds", e.config.TimeoutSeconds)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("command execution failed: %w", err)
		}
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()
	truncated := false

	if len(stdout) > e.config.MaxOutputBytes {
		stdout = stdout[:e.config.MaxOutputBytes]
		truncated = true
	}
	if len(stderr) > e.config.MaxOutputBytes {
		stderr = stderr[:e.config.MaxOutputBytes]
		truncated = true
	}

	return &service.Result{
		Data: &ExecResult{
			Stdout:    stdout,
			Stderr:    stderr,
			ExitCode:  exitCode,
			Truncated: truncated,
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
