# CLI UX Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix broken service-level `--help`, add descriptive help text with examples, fix ExecResult JSON field names, and add fuzzy "did you mean?" suggestions for typos.

**Architecture:** All changes live in two files: `cmd/kuma-approve/main.go` (help text, argument interception, fuzzy matching) and `internal/executor/executor.go` (JSON tags). No new packages, no interface changes. Help text is generated from the existing `ActionDefinition`/`ParamDef` metadata that services already expose. A lightweight help-only registry with nil providers is used to call `Actions()` without loading config/auth.

**Tech Stack:** Go standard library only. No new dependencies.

---

### Task 1: Fix ExecResult JSON tags

**Files:**
- Modify: `internal/executor/executor.go:102-107`
- Test: `internal/executor/executor_test.go`

**Step 1: Write a failing test that asserts snake_case JSON keys**

Add to `internal/executor/executor_test.go`:

```go
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
```

Also add `"encoding/json"` to the test file imports.

**Step 2: Run the test to verify it fails**

Run: `go test ./internal/executor/ -run TestExecResultJSONKeys -v`
Expected: FAIL -- JSON contains capitalized `Stdout` and `ExitCode`.

**Step 3: Add json tags to ExecResult**

In `internal/executor/executor.go`, replace:

```go
type ExecResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}
```

With:

```go
type ExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
}
```

**Step 4: Run the test to verify it passes**

Run: `go test ./internal/executor/ -run TestExecResultJSONKeys -v`
Expected: PASS

**Step 5: Run all executor tests to check for regressions**

Run: `go test ./internal/executor/ -v`
Expected: All tests pass. Existing tests reference `out.ExitCode` and `out.Stdout` as Go field names (not JSON keys), so they are unaffected.

**Step 6: Commit**

```bash
git add internal/executor/executor.go internal/executor/executor_test.go
git commit -m "fix(executor): add snake_case json tags to ExecResult"
```

---

### Task 2: Add Levenshtein distance and closestMatch to main.go

**Files:**
- Modify: `cmd/kuma-approve/main.go`

**Step 1: Write the `levenshtein` and `closestMatch` functions**

Add these functions to `cmd/kuma-approve/main.go` (after `parseFlags`):

```go
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func closestMatch(input string, candidates []string) string {
	best := ""
	bestDist := 3
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(input), strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}
```

Note: Go 1.21+ has a built-in `min` function for integers, so no helper needed.

**Step 2: Verify it compiles**

Run: `go build ./cmd/kuma-approve/`
Expected: Compiles with no errors. The functions are not yet called, so no behavior change.

**Step 3: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "feat(cli): add levenshtein distance and closestMatch helper"
```

---

### Task 3: Rewrite printUsage() with richer help text

**Files:**
- Modify: `cmd/kuma-approve/main.go:306-320`

**Step 1: Replace the `printUsage` function body**

Replace the entire `printUsage` function with:

```go
func printUsage() {
	fmt.Fprintln(os.Stderr, `kuma-approve -- AI-agent CLI for Gmail, Calendar, and shell with Telegram approval

Usage:
  kuma-approve <service> <action> [flags]
  kuma-approve setup
  kuma-approve auth <service> <account>

Services:
  gmail       Gmail operations (list, get, search, send, reply, draft)
  gcal        Google Calendar operations (list, get, create, update, delete)
  outlook     Outlook email operations (list, get, search, send, reply, draft)
  msft-cal    Microsoft Calendar operations (list, get, create, update, delete)
  exec        Shell command execution (run)

Examples:
  kuma-approve gmail list --limit 10
  kuma-approve gcal create --title "Standup" --start 2026-09-05T09:00:00Z --end 2026-09-05T09:30:00Z
  kuma-approve exec run --cmd "df -h"

Run 'kuma-approve <service> --help' for actions, parameters, and examples.
Run 'kuma-approve setup' for first-time configuration.`)
}
```

**Step 2: Build and test manually**

Run: `go build -o kuma-approve ./cmd/kuma-approve/ && ./kuma-approve --help`
Expected: The new help text prints to stderr with services, examples, and guidance. Exit 0.

**Step 3: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "feat(cli): rewrite top-level help with examples and service summaries"
```

---

### Task 4: Add printServiceHelp and service examples map

This is the largest task. It adds the function that generates per-service help from `ActionDefinition` metadata, plus a hardcoded examples map.

**Files:**
- Modify: `cmd/kuma-approve/main.go`

**Step 1: Add the service examples map and `printServiceHelp` function**

Add after `printUsage`:

```go
var serviceDescriptions = map[string]string{
	"gmail":    "Gmail operations",
	"gcal":     "Google Calendar operations",
	"outlook":  "Outlook email operations",
	"msft-cal": "Microsoft Calendar operations",
	"exec":     "Shell command execution",
}

var serviceExamples = map[string][]string{
	"gmail": {
		`kuma-approve gmail list --limit 5`,
		`kuma-approve gmail search --query "from:boss subject:urgent"`,
		`kuma-approve gmail send --to user@example.com --subject "Hello" --body "Hi there"`,
	},
	"gcal": {
		`kuma-approve gcal list --date 2026-09-04`,
		`kuma-approve gcal create --title "Meeting" --start "2026-09-04T10:00:00Z" --end "2026-09-04T11:00:00Z"`,
		`kuma-approve gcal delete --event-id <id>`,
	},
	"outlook": {
		`kuma-approve outlook list --limit 10`,
		`kuma-approve outlook search --query "budget report"`,
		`kuma-approve outlook send --to user@example.com --subject "Hello" --body "Hi there"`,
	},
	"msft-cal": {
		`kuma-approve msft-cal list --date 2026-09-04`,
		`kuma-approve msft-cal create --subject "Standup" --start "2026-09-04T09:00:00Z" --end "2026-09-04T09:30:00Z"`,
		`kuma-approve msft-cal delete --event-id <id>`,
	},
	"exec": {
		`kuma-approve exec run --cmd "df -h"`,
		`kuma-approve exec run --cmd "cat log.txt | grep ERROR" --shell true`,
	},
}

func helpOnlyServices() []service.Service {
	return []service.Service{
		gmail.New(nil, ""),
		gcal.New(nil, ""),
		outlook.New(nil, ""),
		msftcal.New(nil, ""),
	}
}

func printServiceHelp(serviceName string) {
	desc := serviceDescriptions[serviceName]
	if desc == "" {
		desc = serviceName
	}
	fmt.Fprintf(os.Stderr, "%s -- %s\n\nActions:\n", serviceName, desc)

	var actions []service.ActionDefinition

	// Try the help-only services first (no config needed).
	for _, svc := range helpOnlyServices() {
		if svc.Name() == serviceName {
			actions = svc.Actions()
			break
		}
	}

	// Exec is special: needs ExecConfig to construct, but Actions() is still static.
	if serviceName == "exec" && actions == nil {
		execSvc, err := executor.New(executor.ExecConfig{})
		if err == nil {
			actions = execSvc.Actions()
		}
	}

	if actions == nil {
		fmt.Fprintf(os.Stderr, "  (no actions found)\n")
		return
	}

	for _, a := range actions {
		approvalNote := ""
		if a.DefaultTier == approval.TierApprove {
			approvalNote = " (requires approval)"
		}
		fmt.Fprintf(os.Stderr, "  %-10s%s%s\n", a.Name, a.Description, approvalNote)

		for _, p := range a.Params {
			flag := fmt.Sprintf("--%s", p.Name)
			if !p.Required {
				flag = fmt.Sprintf("[--%s]", p.Name)
			}
			fmt.Fprintf(os.Stderr, "            %-13s%s\n", flag, p.Description)
		}
		fmt.Fprintln(os.Stderr)
	}

	if examples, ok := serviceExamples[serviceName]; ok {
		fmt.Fprintln(os.Stderr, "Examples:")
		for _, ex := range examples {
			fmt.Fprintf(os.Stderr, "  %s\n", ex)
		}
	}
}
```

Note: This requires adding `executor` and `approval` imports. The `executor` import is already present via the service wiring. The `approval` import is already present. Verify the `service` import is available (it already is).

**Step 2: Verify it compiles**

Run: `go build ./cmd/kuma-approve/`
Expected: Compiles. The function exists but is not yet called.

**Step 3: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "feat(cli): add printServiceHelp with action metadata and examples"
```

---

### Task 5: Wire up --help interception and missing-action handling

This wires the new functions into the main dispatch flow.

**Files:**
- Modify: `cmd/kuma-approve/main.go:48-55`

**Step 1: Add known-service list and helper**

Add after the imports block (with the other top-level vars):

```go
var knownServices = []string{"gmail", "gcal", "outlook", "msft-cal", "exec"}
```

**Step 2: Replace the missing-action block and add --help interception**

Replace lines 48-55 of `main.go` (the `serviceName` assignment through the `os.Exit(1)` for missing action):

Current code:
```go
	serviceName := command

	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: kuma-approve %s <action> [flags]\n", serviceName)
		os.Exit(1)
	}

	actionName := os.Args[2]
```

Replace with:
```go
	serviceName := command

	// Check for service-level help before requiring an action.
	if len(os.Args) >= 3 {
		arg2 := os.Args[2]
		if arg2 == "--help" || arg2 == "-h" || arg2 == "help" {
			printServiceHelp(serviceName)
			return
		}
	}

	if len(os.Args) < 3 {
		printServiceHelp(serviceName)
		os.Exit(1)
	}

	actionName := os.Args[2]
```

**Step 3: Add unknown service check with fuzzy suggestion before config loading**

Insert right after the `serviceName` assignment and help check, but before config loading. Add before the `actionName` line:

```go
	// Validate service name early, before loading config.
	isKnown := false
	for _, s := range knownServices {
		if s == serviceName {
			isKnown = true
			break
		}
	}
	if !isKnown {
		fmt.Fprintf(os.Stderr, "Error: unknown service %q\n", serviceName)
		if suggestion := closestMatch(serviceName, knownServices); suggestion != "" {
			fmt.Fprintf(os.Stderr, "\nDid you mean %q?\n", suggestion)
		}
		fmt.Fprintf(os.Stderr, "\nAvailable services: %s\n", strings.Join(knownServices, ", "))
		fmt.Fprintf(os.Stderr, "Run 'kuma-approve --help' for usage.\n")
		os.Exit(1)
	}
```

**Step 4: Add unknown action check with fuzzy suggestion**

After `actionName := os.Args[2]` and `args := parseFlags(os.Args[3:])`, add a pre-dispatch action validation. Insert before the `// Load config.` comment:

```go
	// Validate action name early using help-only service instances.
	var validActions []string
	for _, svc := range helpOnlyServices() {
		if svc.Name() == serviceName {
			for _, a := range svc.Actions() {
				validActions = append(validActions, a.Name)
			}
			break
		}
	}
	if serviceName == "exec" && validActions == nil {
		if execSvc, err := executor.New(executor.ExecConfig{}); err == nil {
			for _, a := range execSvc.Actions() {
				validActions = append(validActions, a.Name)
			}
		}
	}
	if validActions != nil {
		found := false
		for _, a := range validActions {
			if a == actionName {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "Error: unknown action %q for service %q\n", actionName, serviceName)
			if suggestion := closestMatch(actionName, validActions); suggestion != "" {
				fmt.Fprintf(os.Stderr, "\nDid you mean %q?\n", suggestion)
			}
			fmt.Fprintf(os.Stderr, "\nAvailable actions: %s\n", strings.Join(validActions, ", "))
			fmt.Fprintf(os.Stderr, "Run 'kuma-approve %s --help' for details.\n", serviceName)
			os.Exit(1)
		}
	}
```

**Step 5: Build and test manually**

Run the following to test each path:

```bash
go build -o kuma-approve ./cmd/kuma-approve/

# Service help via --help (was broken, now works):
./kuma-approve gmail --help

# Service help via -h:
./kuma-approve gmail -h

# Missing action shows service help:
./kuma-approve gmail

# Unknown service with suggestion:
./kuma-approve gmal list

# Unknown action with suggestion:
./kuma-approve gmail sendd --to a@b.com

# Top-level help:
./kuma-approve --help
```

Expected outputs:
- `gmail --help` / `gmail -h` / `gmail`: Full service help with actions, params, examples. Exit 0 for `--help`/`-h`, exit 1 for missing action.
- `gmal list`: `Error: unknown service "gmal"` + `Did you mean "gmail"?` + available services list.
- `gmail sendd`: `Error: unknown action "sendd" for service "gmail"` + `Did you mean "send"?` + available actions list.
- `--help`: New top-level help.

**Step 6: Run full test suite**

Run: `go test ./...`
Expected: All tests pass. The changes only affect CLI argument handling in `main()`, which no tests exercise directly (tests are on individual packages).

**Step 7: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "feat(cli): wire --help interception, fuzzy suggestions, and missing-action help"
```

---

### Task 6: Final verification

**Step 1: Run `go vet`**

Run: `go vet ./...`
Expected: No issues.

**Step 2: Build clean binary and run full manual test**

```bash
go build -o kuma-approve ./cmd/kuma-approve/

# All help paths:
./kuma-approve --help
./kuma-approve gmail --help
./kuma-approve gcal --help
./kuma-approve outlook --help
./kuma-approve msft-cal --help
./kuma-approve exec --help

# Error paths:
./kuma-approve gmal list
./kuma-approve gmail sendd
./kuma-approve gmail
./kuma-approve

# Verify exec JSON output has snake_case keys:
./kuma-approve exec run --cmd "echo test" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(list(d['data'].keys()))"
```

Expected for the last command: `['stdout', 'stderr', 'exit_code', 'truncated']`

Note: The exec command will fail if kuma-approve is not set up (no config). That is expected. The JSON tag fix can be verified via the unit test from Task 1 instead.

**Step 3: Run full test suite one final time**

Run: `go test ./... -v`
Expected: All tests pass.
