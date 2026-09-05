# Dispatch Pipeline Fixes Design

Date: 2026-09-06

## Problem

Agent testing surfaced five issues in the CLI's dispatch pipeline:

1. **Reject-code collapse**: Human veto returns `DISPATCH_ERROR` instead of `APPROVAL_REJECTED`. An agent can't distinguish "a human said no" from "you misspelled the action" and may retry a vetoed request.
2. **Gate-before-validate**: The approval gate fires before parameter validation. Users get approval prompts for requests that will fail (e.g., `gmail send` with no `--to`).
3. **Envelope break**: Unknown service/action errors print prose to stderr with empty stdout. The "stdout is always JSON" contract breaks at the case where agents most need parseable output.
4. **INVALID_ARGS dead code**: Nothing emits `INVALID_ARGS` as a user-visible error code. Missing required params surface as `DISPATCH_ERROR`.
5. **Fuzzy match threshold too tight**: `msft` gets no suggestion despite `msft-cal` existing (levenshtein distance 4, threshold is 3).

## Approach

Router-centric validation (Approach A): move parameter validation into the router before the approval gate, and make the router return typed errors with distinct error codes instead of plain `fmt.Errorf` that main.go collapses into `DISPATCH_ERROR`.

## Design

### 1. Typed Router Errors

Introduce a `RouterError` type in `internal/cli/router.go`:

```go
type RouterError struct {
    Code    string
    Message string
    Err     error
}

func (e *RouterError) Error() string { return e.Message }
func (e *RouterError) Unwrap() error { return e.Err }
```

The router wraps every error return with the correct code. `main.go` checks for `*RouterError` and passes `Code` and `Message` into the envelope. No more blanket `DISPATCH_ERROR` wrapper.

### 2. Error Code Taxonomy

User-visible codes on stdout:

| Code | Meaning | Agent behavior |
|---|---|---|
| `UNKNOWN_SERVICE` | Service name not recognized | Check spelling, use did_you_mean |
| `UNKNOWN_ACTION` | Action not recognized for service | Check spelling, use did_you_mean |
| `INVALID_ARGS` | Missing or invalid required params | Fix the call and retry |
| `APPROVAL_REJECTED` | Human explicitly vetoed the request | Do NOT retry; ask the user |
| `APPROVAL_TIMEOUT` | Approval timed out | May retry after delay |
| `APPROVAL_ERROR` | Approval system failure | Retry with backoff |
| `NO_APPROVER` | Needs approval but none configured | Config issue, don't retry |
| `DENIED_BY_POLICY` | Action on deny list | Never retry |
| `AUTH_EXPIRED` | OAuth token expired | Run auth, then retry |
| `EXECUTION_TIMEOUT` | Action execution timed out | May retry |
| `API_ERROR` | Upstream API failure | Retry with backoff |
| `DISPATCH_ERROR` | Catch-all for unexpected errors | Investigate |

`DISPATCH_ERROR` becomes the catch-all only for errors that don't match any specific code.

### 3. Validate-Before-Approve Pipeline

New dispatch pipeline order:

1. `GetAction()` -- lookup service and action
2. `validateParams()` -- check required params against `ActionDefinition.Params`
3. `ResolveTier()` -- determine approval requirement
4. Request approval (if tier = approve)
5. `Execute()` -- run the action

```go
func validateParams(action ActionDefinition, args map[string]string) error {
    var missing []string
    for _, p := range action.Params {
        if p.Required {
            if v, ok := args[p.Name]; !ok || v == "" {
                missing = append(missing, p.Name)
            }
        }
    }
    if len(missing) > 0 {
        return fmt.Errorf("missing required parameter(s): %s", strings.Join(missing, ", "))
    }
    return nil
}
```

Services keep their validation inside `Execute()` as defense-in-depth. No service code is removed.

### 4. Envelope Consistency

Replace stderr prose blocks in `main.go` (unknown service at lines 61-68, unknown action at lines 113-120) with `output.Fail` calls using `UNKNOWN_SERVICE` / `UNKNOWN_ACTION` codes.

Add `DidYouMean` to `ErrorInfo`:

```go
type ErrorInfo struct {
    Code       string `json:"code"`
    Message    string `json:"message"`
    DidYouMean string `json:"did_you_mean,omitempty"`
}
```

### 5. Approval Cancellation on CLI Exit

Add `CancelApproval(messageID)` to `TelegramApprover`:

```go
func (t *TelegramApprover) CancelApproval(messageID int64) {
    t.apiCall("editMessageText", url.Values{
        "chat_id":    {t.config.ChatID},
        "message_id": {fmt.Sprintf("%d", messageID)},
        "text":       {"[Cancelled] This approval request is no longer active."},
        "reply_markup": {`{"inline_keyboard":[]}`},
    })
}
```

In `RequestApproval`, add a defer after sending the message:

```go
defer func() {
    if ctx.Err() != nil {
        t.CancelApproval(messageID)
    }
}()
```

This covers timeout and SIGINT. Validate-before-approve eliminates the "validation failure after approval sent" scenario.

### 6. Prefix-Aware Fuzzy Matching

Add prefix matching to `closestMatch` alongside existing levenshtein:

- If `len(input) >= 3` and input is a prefix of a candidate (or vice versa), it qualifies as a match.
- Among all matches (prefix and levenshtein), prefer the shortest distance.
- `msft` matches `msft-cal` via prefix. `g` does not match anything (too short).

## Test Plan

1. **Router error codes**: Test `Dispatch` returns `*RouterError` with correct code for each failure mode.
2. **Validate-before-approve**: Test that mock approver is never called when required params are missing. Confirm `INVALID_ARGS` code.
3. **Envelope consistency**: Run binary with unknown service/action, confirm JSON on stdout with correct codes and `did_you_mean`.
4. **Approval cancellation**: Test `CancelApproval` sends expected API call. Test context cancellation triggers cleanup.
5. **Prefix matching**: Test `closestMatch("msft", ...) == "msft-cal"`, `closestMatch("gm", ...) == ""`, `closestMatch("outloo", ...) == "outlook"`.
6. **Mail digest verification**: Manual test confirming `gmail send` with all flags shows to/subject/body in Telegram Details block.

## Out of Scope

- Improved mail digest rendering (verify current rendering works, don't gold-plate)
- TTY detection for human-friendly output (always JSON on stdout)
- Late-tap safety on stale approval buttons (cancellation eliminates the scenario)
