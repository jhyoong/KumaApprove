# Dispatch Pipeline Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the five dispatch pipeline bugs surfaced by agent testing: error-code collapse, gate-before-validate, envelope break, INVALID_ARGS dead code, and fuzzy match threshold.

**Architecture:** Add a typed `RouterError` to the router so each failure mode carries its own error code. Insert a `validateParams` step before the approval gate. Replace stderr prose in main.go with JSON envelope output. Add approval cancellation on context exit. Add prefix matching to fuzzy suggestions.

**Tech Stack:** Go 1.26.4, no external dependencies added.

---

### Task 1: Add RouterError type to router.go

**Files:**
- Modify: `internal/cli/router.go:1-14` (add type after imports)

**Step 1: Write the failing test**

Add to `internal/cli/router_test.go` at the end of the file:

```go
func TestRouterErrorType(t *testing.T) {
	reg := service.NewRegistry()
	r := NewRouter(RouterConfig{Registry: reg})

	_, err := r.Dispatch("nonexistent", "list", "", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T: %v", err, err)
	}
	if re.Code == "" {
		t.Fatal("expected non-empty error code")
	}
}
```

Also add `"errors"` to the test file's import block.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRouterErrorType -v`
Expected: FAIL -- `RouterError` type does not exist yet.

**Step 3: Write minimal implementation**

Add the `RouterError` type to `internal/cli/router.go` after the existing imports and before `RouterConfig`:

```go
// RouterError carries a machine-readable error code from the dispatch pipeline.
type RouterError struct {
	Code    string
	Message string
	Err     error
}

func (e *RouterError) Error() string { return e.Message }
func (e *RouterError) Unwrap() error { return e.Err }
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestRouterErrorType -v`
Expected: Still FAIL -- `Dispatch` returns plain `error`, not `*RouterError`. That's expected; the next task wires it up.

**Step 5: Commit**

```bash
git add internal/cli/router.go internal/cli/router_test.go
git commit -m "feat(cli): add RouterError type for typed dispatch errors"
```

---

### Task 2: Wire RouterError into all Dispatch error paths

**Files:**
- Modify: `internal/cli/router.go` (the `Dispatch` method, lines 42-128)

**Step 1: Write the failing tests**

Add to `internal/cli/router_test.go`:

```go
func TestRouterErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		service      string
		action       string
		args         map[string]string
		setupReg     func() *service.Registry
		approver     *fakeApprover
		tierOverride map[string]string
		wantCode     string
	}{
		{
			name:    "unknown service",
			service: "bogus", action: "list",
			setupReg: func() *service.Registry { return service.NewRegistry() },
			wantCode: "INVALID_ARGS",
		},
		{
			name:    "unknown action",
			service: "gmail", action: "bogus",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "list", DefaultTier: "auto"}},
				})
				return reg
			},
			wantCode: "INVALID_ARGS",
		},
		{
			name:    "denied by policy",
			service: "gmail", action: "list",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "list", DefaultTier: "auto"}},
				})
				return reg
			},
			tierOverride: map[string]string{"gmail:list": "deny"},
			wantCode:     "DENIED_BY_POLICY",
		},
		{
			name:    "no approver configured",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			wantCode: "NO_APPROVER",
		},
		{
			name:    "approval rejected",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			approver: &fakeApprover{approved: false},
			wantCode: "APPROVAL_REJECTED",
		},
		{
			name:    "approval error",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			approver: &fakeApprover{err: fmt.Errorf("network down")},
			wantCode: "APPROVAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RouterConfig{
				Registry:      tt.setupReg(),
				TierOverrides: tt.tierOverride,
			}
			if tt.approver != nil {
				cfg.Approver = tt.approver
			}
			r := NewRouter(cfg)

			_, err := r.Dispatch(tt.service, tt.action, "user@test.com", tt.args)
			if err == nil {
				t.Fatal("expected error")
			}

			var re *RouterError
			if !errors.As(err, &re) {
				t.Fatalf("expected *RouterError, got %T: %v", err, err)
			}
			if re.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s (msg: %s)", tt.wantCode, re.Code, re.Message)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestRouterErrorCodes -v`
Expected: FAIL -- `Dispatch` still returns plain errors, not `*RouterError`.

**Step 3: Modify Dispatch to return RouterError**

Replace all error returns in `Dispatch` (`internal/cli/router.go`) with `*RouterError`. The full updated `Dispatch` method:

```go
func (r *Router) Dispatch(serviceName, actionName, account string, args map[string]string) (any, error) {
	actionKey := serviceName + ":" + actionName

	// Look up service and action.
	actionDef, err := r.config.Registry.GetAction(serviceName, actionName)
	if err != nil {
		r.logAction(actionKey, account, args, "denied", "failure", "INVALID_ARGS")
		return nil, &RouterError{Code: "INVALID_ARGS", Message: err.Error(), Err: err}
	}

	// Resolve tier.
	tier := approval.ResolveTier(actionKey, actionDef.DefaultTier, r.config.TierOverrides)

	// Deny tier.
	if tier == approval.TierDeny {
		r.logAction(actionKey, account, args, "denied", "failure", "DENIED_BY_POLICY")
		return nil, &RouterError{Code: "DENIED_BY_POLICY", Message: fmt.Sprintf("action %s denied by policy", actionKey)}
	}

	// Approve tier -- requires approver.
	if tier == approval.TierApprove {
		if r.config.Approver == nil {
			r.logAction(actionKey, account, args, "denied", "failure", "NO_APPROVER")
			return nil, &RouterError{Code: "NO_APPROVER", Message: fmt.Sprintf("action %s requires approval but no approver configured", actionKey)}
		}

		timeout := time.Duration(r.config.TimeoutMinutes) * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		result, err := r.config.Approver.RequestApproval(ctx, approval.ApprovalRequest{
			Action:  actionKey,
			Account: account,
			Details: args,
		})
		if err != nil {
			errCode := "APPROVAL_ERROR"
			if ctx.Err() == context.DeadlineExceeded {
				errCode = "APPROVAL_TIMEOUT"
			}
			r.logAction(actionKey, account, args, "error", "failure", errCode)
			return nil, &RouterError{Code: errCode, Message: fmt.Sprintf("approval failed: %v", err), Err: err}
		}
		if !result.Approved {
			r.logAction(actionKey, account, args, "rejected", "failure", "APPROVAL_REJECTED")
			return nil, &RouterError{Code: "APPROVAL_REJECTED", Message: fmt.Sprintf("action %s was rejected", actionKey)}
		}
	}

	// Execute the action.
	svc := r.config.Registry.Get(serviceName)
	svcResult, err := svc.Execute(actionName, args)
	if err != nil {
		status := "auto"
		if tier == approval.TierApprove {
			status = "approved"
		}

		var authErr *auth.AuthExpiredError
		if errors.As(err, &authErr) {
			if r.config.Notifier != nil {
				msg := fmt.Sprintf("Auth for %s:%s has expired. Run `kuma-approve auth %s %s` to re-authorize.",
					authErr.Service, authErr.Account, authErr.Service, authErr.Account)
				r.config.Notifier.SendMessage(msg)
			}
			r.logAction(actionKey, account, args, status, "failure", "AUTH_EXPIRED")
			return nil, &RouterError{Code: "AUTH_EXPIRED", Message: err.Error(), Err: err}
		}

		var timeoutErr *executor.TimeoutError
		if errors.As(err, &timeoutErr) {
			r.logAction(actionKey, account, args, status, "failure", "EXECUTION_TIMEOUT")
			return nil, &RouterError{Code: "EXECUTION_TIMEOUT", Message: err.Error(), Err: err}
		}

		r.logAction(actionKey, account, args, status, "failure", "API_ERROR")
		return nil, &RouterError{Code: "API_ERROR", Message: err.Error(), Err: err}
	}

	status := "auto"
	if tier == approval.TierApprove {
		status = "approved"
	}
	r.logAction(actionKey, account, args, status, "success", "")

	return svcResult.Data, nil
}
```

**Step 4: Run all router tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: All tests pass. Existing tests check error messages via `strings.Contains`, which still works since `RouterError.Error()` returns the `Message` field.

**Step 5: Commit**

```bash
git add internal/cli/router.go internal/cli/router_test.go
git commit -m "feat(cli): return RouterError with typed codes from all Dispatch paths"
```

---

### Task 3: Add validate-before-approve to the router

**Files:**
- Modify: `internal/cli/router.go` (add `validateParams` function, insert call in `Dispatch`)
- Modify: `internal/cli/router_test.go` (add tests)

**Step 1: Write the failing tests**

Add to `internal/cli/router_test.go`:

```go
func TestRouterValidateBeforeApprove(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "send",
				DefaultTier: "approve",
				Params: []service.ParamDef{
					{Name: "to", Required: true, Description: "Recipient"},
					{Name: "subject", Required: true, Description: "Subject"},
					{Name: "body", Required: true, Description: "Body"},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	// Missing all required params -- should fail with INVALID_ARGS
	// and approver should NOT be called.
	_, err := r.Dispatch("gmail", "send", "user@test.com", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T", err)
	}
	if re.Code != "INVALID_ARGS" {
		t.Fatalf("expected INVALID_ARGS, got %s", re.Code)
	}
	if approver.called {
		t.Fatal("approver should NOT be called when params are missing")
	}
}

func TestRouterValidatePassesWithAllParams(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "send",
				DefaultTier: "approve",
				Params: []service.ParamDef{
					{Name: "to", Required: true},
					{Name: "subject", Required: true},
					{Name: "body", Required: true},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gmail", "send", "user@test.com", map[string]string{
		"to":      "a@b.com",
		"subject": "Hi",
		"body":    "Hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !approver.called {
		t.Fatal("expected approver to be called")
	}
}

func TestRouterValidateOptionalParamsNotRequired(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "list",
				DefaultTier: "auto",
				Params: []service.ParamDef{
					{Name: "limit", Required: false},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	r := NewRouter(RouterConfig{Registry: reg})

	// No params at all -- should succeed since "limit" is optional.
	_, err := r.Dispatch("gmail", "list", "user@test.com", map[string]string{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestRouterValidate -v`
Expected: FAIL -- `TestRouterValidateBeforeApprove` fails because the approver IS called (no validation step yet).

**Step 3: Add validateParams and insert it into Dispatch**

Add the `validateParams` function to `internal/cli/router.go` (before the `Dispatch` method). Also add `"strings"` to imports if not already there:

```go
func validateParams(actionDef service.ActionDefinition, args map[string]string) []string {
	var missing []string
	for _, p := range actionDef.Params {
		if p.Required {
			if v, ok := args[p.Name]; !ok || v == "" {
				missing = append(missing, p.Name)
			}
		}
	}
	return missing
}
```

In the `Dispatch` method, insert the validation call immediately after `GetAction` succeeds and before `ResolveTier`. Add between the existing `GetAction` block and the `ResolveTier` line:

```go
	// Validate required parameters before approval gate.
	if missing := validateParams(actionDef, args); len(missing) > 0 {
		msg := fmt.Sprintf("missing required parameter(s): %s", strings.Join(missing, ", "))
		r.logAction(actionKey, account, args, "denied", "failure", "INVALID_ARGS")
		return nil, &RouterError{Code: "INVALID_ARGS", Message: msg}
	}
```

**Step 4: Run all router tests**

Run: `go test ./internal/cli/ -v`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/cli/router.go internal/cli/router_test.go
git commit -m "feat(cli): validate required params before approval gate"
```

---

### Task 4: Wire RouterError codes through main.go into the JSON envelope

**Files:**
- Modify: `cmd/kuma-approve/main.go:250-260` (the `router.Dispatch` error handling)

**Step 1: Write the failing test**

This is an integration-level check. We can't easily unit test main.go, so verify by reading the code change carefully and then running the full test suite. But first, confirm the current behavior is wrong by examining the code.

Current code at `main.go:252-258`:
```go
result, err := router.Dispatch(serviceName, actionName, account, args)
if err != nil {
    output.PrintAndExit(output.Fail(
        serviceName+":"+actionName,
        "DISPATCH_ERROR",
        err.Error(),
    ))
    return
}
```

This wraps every router error as `DISPATCH_ERROR` regardless of the `RouterError.Code`.

**Step 2: Update main.go to extract RouterError codes**

Replace the error handling block after `router.Dispatch` in `main.go` with:

```go
	result, err := router.Dispatch(serviceName, actionName, account, args)
	if err != nil {
		code := "DISPATCH_ERROR"
		var re *cli.RouterError
		if errors.As(err, &re) {
			code = re.Code
		}
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			code,
			err.Error(),
		))
		return
	}
```

Also add `"errors"` to the import block in main.go if not already present.

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "feat(cli): pass RouterError codes through to JSON envelope"
```

---

### Task 5: Add DidYouMean to ErrorInfo and convert unknown service/action to JSON envelope

**Files:**
- Modify: `internal/output/output.go` (add `DidYouMean` field to `ErrorInfo`)
- Modify: `internal/output/output_test.go` (test the new field)
- Modify: `cmd/kuma-approve/main.go:52-68, 104-121` (replace stderr prose with JSON envelope)

**Step 1: Write the failing test**

Add to `internal/output/output_test.go`:

```go
func TestErrorEnvelopeDidYouMean(t *testing.T) {
	env := Fail("bogus:unknown", "UNKNOWN_SERVICE", "unknown service \"bogus\"")
	env.Error.DidYouMean = "gmail"

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	json.Unmarshal(b, &raw)

	errObj := raw["error"].(map[string]any)
	if errObj["did_you_mean"] != "gmail" {
		t.Fatalf("expected did_you_mean=gmail, got %v", errObj["did_you_mean"])
	}
}

func TestErrorEnvelopeDidYouMeanOmitted(t *testing.T) {
	env := Fail("gmail:send", "API_ERROR", "something broke")

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	json.Unmarshal(b, &raw)

	errObj := raw["error"].(map[string]any)
	if _, exists := errObj["did_you_mean"]; exists {
		t.Fatal("expected did_you_mean to be omitted when empty")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/output/ -run TestErrorEnvelopeDidYouMean -v`
Expected: FAIL -- `DidYouMean` field does not exist on `ErrorInfo`.

**Step 3: Add DidYouMean to ErrorInfo**

In `internal/output/output.go`, update the `ErrorInfo` struct:

```go
type ErrorInfo struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	DidYouMean string `json:"did_you_mean,omitempty"`
}
```

**Step 4: Run output tests**

Run: `go test ./internal/output/ -v`
Expected: All pass.

**Step 5: Replace stderr prose in main.go for unknown service**

Replace the unknown service block in `main.go` (the `if !isKnown` block, approximately lines 60-68):

```go
	if !isKnown {
		env := output.Fail(
			serviceName+":unknown",
			"UNKNOWN_SERVICE",
			fmt.Sprintf("unknown service %q; available: %s", serviceName, strings.Join(knownServices, ", ")),
		)
		if suggestion := closestMatch(serviceName, knownServices); suggestion != "" {
			env.Error.DidYouMean = suggestion
		}
		output.PrintAndExit(env)
		return
	}
```

**Step 6: Replace stderr prose in main.go for unknown action**

Replace the unknown action block in `main.go` (the `if !found` block, approximately lines 112-120):

```go
		if !found {
			env := output.Fail(
				serviceName+":"+actionName,
				"UNKNOWN_ACTION",
				fmt.Sprintf("unknown action %q for service %q; available: %s", actionName, serviceName, strings.Join(validActions, ", ")),
			)
			if suggestion := closestMatch(actionName, validActions); suggestion != "" {
				env.Error.DidYouMean = suggestion
			}
			output.PrintAndExit(env)
			return
		}
```

**Step 7: Run full test suite**

Run: `go test ./...`
Expected: All pass.

**Step 8: Commit**

```bash
git add internal/output/output.go internal/output/output_test.go cmd/kuma-approve/main.go
git commit -m "feat(output): add did_you_mean field, convert unknown service/action to JSON envelope"
```

---

### Task 6: Update output_test.go error code list

**Files:**
- Modify: `internal/output/output_test.go` (update `TestErrorCodes` to include new codes)

**Step 1: Update the valid codes map**

The existing `TestErrorCodes` test in `internal/output/output_test.go` lists only 7 codes. Update it to include all codes from the taxonomy:

```go
func TestErrorCodes(t *testing.T) {
	valid := map[string]bool{
		"UNKNOWN_SERVICE":    true,
		"UNKNOWN_ACTION":     true,
		"INVALID_ARGS":       true,
		"APPROVAL_REJECTED":  true,
		"APPROVAL_TIMEOUT":   true,
		"APPROVAL_ERROR":     true,
		"NO_APPROVER":        true,
		"DENIED_BY_POLICY":   true,
		"AUTH_EXPIRED":       true,
		"EXECUTION_TIMEOUT":  true,
		"API_ERROR":          true,
		"DISPATCH_ERROR":     true,
		"NO_ACCOUNT":         true,
		"CONFIG_ERROR":       true,
		"MACHINE_ID_ERROR":   true,
		"KEY_ERROR":          true,
		"CREDSTORE_ERROR":    true,
		"EXEC_INIT_ERROR":    true,
		"AUDIT_ERROR":        true,
	}
	for code := range valid {
		env := Fail("test", code, "msg")
		if env.Error.Code != code {
			t.Fatalf("code mismatch: %s", code)
		}
	}
}
```

**Step 2: Run output tests**

Run: `go test ./internal/output/ -v`
Expected: All pass.

**Step 3: Commit**

```bash
git add internal/output/output_test.go
git commit -m "test(output): update error code list to include full taxonomy"
```

---

### Task 7: Add approval cancellation to TelegramApprover

**Files:**
- Modify: `internal/approval/telegram.go` (add `CancelApproval`, update `RequestApproval`)
- Modify: `internal/approval/telegram_test.go` (add cancellation tests)

**Step 1: Write the failing test for CancelApproval**

Add to `internal/approval/telegram_test.go`:

```go
func TestTelegramCancelApproval(t *testing.T) {
	var mu sync.Mutex
	var editCalled bool
	var editText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/botsecret/editMessageText":
			editCalled = true
			r.ParseForm()
			editText = r.FormValue("text")
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		}
	}))
	defer server.Close()

	tg := NewTelegramApprover(TelegramConfig{
		BotToken: "secret",
		ChatID:   "123",
		BaseURL:  server.URL,
	})

	tg.CancelApproval(42)

	mu.Lock()
	defer mu.Unlock()
	if !editCalled {
		t.Fatal("expected editMessageText to be called")
	}
	if !strings.Contains(editText, "Cancelled") {
		t.Fatalf("expected 'Cancelled' in edit text, got: %s", editText)
	}
}
```

Also add `"strings"` to the test file's import block if not already present.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/approval/ -run TestTelegramCancelApproval -v`
Expected: FAIL -- `CancelApproval` method does not exist.

**Step 3: Add CancelApproval method**

Add to `internal/approval/telegram.go`, after the `removeKeyboard` method:

```go
// CancelApproval marks a pending approval message as cancelled and removes the keyboard.
func (t *TelegramApprover) CancelApproval(messageID int64) {
	t.apiCall("editMessageText", url.Values{
		"chat_id":      {t.config.ChatID},
		"message_id":   {fmt.Sprintf("%d", messageID)},
		"text":         {"[Cancelled] This approval request is no longer active."},
		"parse_mode":   {"Markdown"},
		"reply_markup": {`{"inline_keyboard":[]}`},
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/approval/ -run TestTelegramCancelApproval -v`
Expected: PASS.

**Step 5: Write the failing test for cancellation on context exit**

Add to `internal/approval/telegram_test.go`:

```go
func TestTelegramCancelOnContextExit(t *testing.T) {
	var mu sync.Mutex
	var editTextCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/botsecret/sendMessage":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 99},
			})
		case r.URL.Path == "/botsecret/getUpdates":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": []any{},
			})
		case r.URL.Path == "/botsecret/editMessageText":
			editTextCalled = true
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	tg := NewTelegramApprover(TelegramConfig{
		BotToken:     "secret",
		ChatID:       "123",
		BaseURL:      server.URL,
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := tg.RequestApproval(ctx, ApprovalRequest{
		Action: "gmail:send",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Give a moment for the deferred cancel call to execute.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !editTextCalled {
		t.Fatal("expected editMessageText to be called for cancellation")
	}
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./internal/approval/ -run TestTelegramCancelOnContextExit -v`
Expected: FAIL -- `RequestApproval` does not call `CancelApproval` on context exit yet.

**Step 7: Add defer-based cancellation to RequestApproval**

In `internal/approval/telegram.go`, update the `RequestApproval` method. After the `sendApprovalMessage` call, add the cancellation defer:

```go
func (t *TelegramApprover) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	if req.RequestID == "" {
		req.RequestID = generateRequestID()
	}

	messageID, err := t.sendApprovalMessage(req)
	if err != nil {
		return ApprovalResult{}, fmt.Errorf("sending telegram message: %w", err)
	}

	defer func() {
		if ctx.Err() != nil {
			t.CancelApproval(messageID)
		}
	}()

	return t.pollForResponse(ctx, req.RequestID, messageID)
}
```

**Step 8: Run all approval tests**

Run: `go test ./internal/approval/ -v`
Expected: All pass (including the existing `TestTelegramTimeout` test, which should now also trigger the cancel path).

**Step 9: Commit**

```bash
git add internal/approval/telegram.go internal/approval/telegram_test.go
git commit -m "feat(approval): add CancelApproval and auto-cancel on context exit"
```

---

### Task 8: Add prefix matching to closestMatch

**Files:**
- Modify: `cmd/kuma-approve/main.go` (update `closestMatch` function)

**Step 1: Write the failing tests**

There's no separate test file for main.go helpers. These functions are unexported, so we need a test file in the same package. Create `cmd/kuma-approve/main_test.go`:

```go
package main

import "testing"

func TestClosestMatchPrefix(t *testing.T) {
	services := []string{"gmail", "gcal", "outlook", "msft-cal", "exec"}

	tests := []struct {
		input string
		want  string
	}{
		{"msft", "msft-cal"},
		{"outloo", "outlook"},
		{"gmai", "gmail"},          // levenshtein 1, still works
		{"xyz", ""},                // no match
		{"gm", ""},                 // too short for prefix, levenshtein too high
		{"ex", ""},                 // too short for prefix
		{"exe", "exec"},            // prefix match at minimum length
		{"msft-cal", "msft-cal"},   // exact match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := closestMatch(tt.input, services)
			if got != tt.want {
				t.Fatalf("closestMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify some fail**

Run: `go test ./cmd/kuma-approve/ -run TestClosestMatchPrefix -v`
Expected: FAIL for `msft` and `outloo` cases (no prefix matching yet).

**Step 3: Update closestMatch to include prefix matching**

Replace the `closestMatch` function in `cmd/kuma-approve/main.go`:

```go
func closestMatch(input string, candidates []string) string {
	best := ""
	bestDist := 3
	inputLower := strings.ToLower(input)

	for _, c := range candidates {
		cLower := strings.ToLower(c)

		d := levenshtein(inputLower, cLower)

		// Prefix match: input is a prefix of candidate or vice versa.
		// Require at least 3 chars to avoid noisy single/two-letter matches.
		if len(inputLower) >= 3 && (strings.HasPrefix(cLower, inputLower) || strings.HasPrefix(inputLower, cLower)) {
			if best == "" || d < bestDist {
				bestDist = d
				best = c
			}
			continue
		}

		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}
```

**Step 4: Run all tests**

Run: `go test ./cmd/kuma-approve/ -run TestClosestMatchPrefix -v`
Expected: All pass.

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: All pass.

**Step 6: Commit**

```bash
git add cmd/kuma-approve/main.go cmd/kuma-approve/main_test.go
git commit -m "feat(cli): add prefix matching to closestMatch for fuzzy suggestions"
```

---

### Task 9: Final verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests pass.

**Step 2: Build the binary**

Run: `go build -o kuma-approve ./cmd/kuma-approve/`
Expected: Builds cleanly with no errors.

**Step 3: Verify unknown service JSON output**

Run: `./kuma-approve bogus list 2>/dev/null`
Expected: JSON on stdout with `"code":"UNKNOWN_SERVICE"` and exit 1.

**Step 4: Verify unknown action JSON output**

Run: `./kuma-approve gmail bogus 2>/dev/null`
Expected: JSON on stdout with `"code":"UNKNOWN_ACTION"` and exit 1.

**Step 5: Verify prefix suggestion**

Run: `./kuma-approve msft list 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['error']['did_you_mean'])"`
Expected: prints `msft-cal`.

**Step 6: Verify go vet**

Run: `go vet ./...`
Expected: No issues.

**Step 7: Commit (if any cleanup was needed)**

Only commit if changes were made during verification.
