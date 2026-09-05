# CLI UX Improvements Design

## Problem

The CLI has several usability issues that affect both human users and AI agents:

1. **Service-level `--help` is broken.** `kuma-approve gmail --help` treats `--help` as an action name, loads config/credstore/auth, then fails with `DISPATCH_ERROR: "unknown action --help for service gmail"`. The top-level help text directs users into this trap.

2. **ExecResult uses capitalized Go field names in JSON.** The `ExecResult` struct has no `json:` tags, producing `Stdout`, `ExitCode` instead of `stdout`, `exit_code`. All other service result structs have proper tags; this is the only outlier.

3. **Top-level help is bare-bones.** No examples, no quick-start guidance. Lists `config` as a service even though it doesn't exist.

4. **`kuma-approve <service>` (no action) prints a bare usage line** that doesn't list available actions or parameters.

5. **Error messages are terse.** "unknown service: gmal" with no suggestions or hints about valid options.

## Decisions

- **Audience:** Both AI agents and human users.
- **Help output format:** Human-readable text to stderr, exit 0. No JSON envelope for help.
- **JSON field casing:** snake_case (`stdout`, `exit_code`) for ExecResult.
- **`config` service:** Remove from help text (not implemented, no plan to build).
- **Unknown service/action errors:** Fuzzy matching with "did you mean?" suggestions using Levenshtein distance.

## Approach

All changes in `main.go` and `executor.go`. No new packages, no interface changes, no new dependencies.

## Design

### 1. Service-level `--help` interception

In `main.go`, after determining `serviceName` and before requiring a second argument, check if any of `os.Args[2:]` is `--help`, `-h`, or `help`. If matched, call `printServiceHelp(serviceName)` and exit 0.

`printServiceHelp` builds a lightweight help-only registry by constructing services with nil token providers and empty account strings. This is safe because `Actions()` is a pure method returning static slices on every service -- it never touches the token provider or account fields.

Output format (stderr):

```
gmail -- Gmail operations

Actions:
  list      List recent messages
            [--limit]    Max messages to return (default 20)

  get       Get a message by ID
            --id         Message ID

  send      Send a new email (requires approval)
            --to         Recipient email
            --subject    Email subject
            --body       Email body

Examples:
  kuma-approve gmail list --limit 5
  kuma-approve gmail send --to user@example.com --subject "Hello" --body "Hi there"
```

Required params shown as `--name`, optional as `[--name]`. Actions with tier "approve" get a "(requires approval)" note.

### 2. Fix ExecResult JSON tags

Add `json:"snake_case"` tags to `ExecResult` in `internal/executor/executor.go`:

```go
type ExecResult struct {
    Stdout    string `json:"stdout"`
    Stderr    string `json:"stderr"`
    ExitCode  int    `json:"exit_code"`
    Truncated bool   `json:"truncated"`
}
```

All other service result structs already have proper tags. This is the only fix needed.

### 3. Improved top-level help

Replace `printUsage()` with a richer version:

```
kuma-approve -- AI-agent CLI for Gmail, Calendar, and shell with Telegram approval

Usage:
  kuma-approve <service> <action> [flags]
  kuma-approve setup
  kuma-approve auth <service> <account>

Services:
  gmail       Gmail operations (list, search, send, reply, draft)
  gcal        Google Calendar operations (list, create, update, delete)
  outlook     Outlook email operations (list, search, send, reply, draft)
  msft-cal    Microsoft Calendar operations (list, create, update, delete)
  exec        Shell command execution (run)

Examples:
  kuma-approve gmail list --limit 10
  kuma-approve gcal create --title "Standup" --start 2026-09-05T09:00:00Z --end 2026-09-05T09:30:00Z
  kuma-approve exec run --cmd "df -h"

Run 'kuma-approve <service> --help' for actions, parameters, and examples.
Run 'kuma-approve setup' for first-time configuration.
```

Changes from current: `config` removed, `setup` and `auth` shown as distinct usage forms, each service line shows action names in parens, real examples included.

### 4. Fuzzy matching for unknown service/action

Add a `closestMatch(input string, candidates []string) string` function using Levenshtein distance. If the best match has edit distance <= 2, suggest it. ~20 lines, no external dependency.

**Unknown service** (checked in main.go before dispatch):
```
Error: unknown service "gmal"

Did you mean "gmail"?

Available services: gmail, gcal, outlook, msft-cal, exec
Run 'kuma-approve --help' for usage.
```

**Unknown action** (checked in main.go before dispatch):
```
Error: unknown action "sendd" for service "gmail"

Did you mean "send"?

Available actions: list, get, search, send, reply, draft
Run 'kuma-approve gmail --help' for details.
```

Both output to stderr, exit 1, no JSON envelope. These are user input errors, not runtime failures.

### 5. Missing action shows service help

When `len(os.Args) < 3` and the command matches a known service, call `printServiceHelp()` instead of the current bare `usage:` line. Exit 1 (missing required argument). Reuses the function from change 1.

## Files affected

| File | Change |
|------|--------|
| `cmd/kuma-approve/main.go` | Help interception, printUsage rewrite, printServiceHelp, closestMatch, missing-action handling |
| `internal/executor/executor.go` | Add json tags to ExecResult |

## Out of scope

- No `config` service implementation.
- No `--json` flag for structured help output.
- No `--version` flag (can be added later).
- No changes to the `Service` interface, registry, or router.
