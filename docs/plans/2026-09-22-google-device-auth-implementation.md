# Google Device Authorization Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When a Google token refresh fails, automatically fall back to the device authorization grant (RFC 8628) so the user can re-authorize from any device without needing a browser on the work machine.

**Architecture:** All changes live in `internal/auth/google.go`. `GetToken()` gains a device-flow fallback between refresh failure and `AuthExpiredError`. Three new private methods handle the device code request, stderr output, and polling loop. The stdout JSON envelope contract is unchanged.

**Tech Stack:** Go stdlib (`net/http`, `net/url`, `encoding/json`, `fmt`, `time`, `os`). No new dependencies.

---

### Task 1: Add `requestDeviceCode()` with tests

**Files:**
- Modify: `internal/auth/google.go` (add method after `refreshToken()` around line 126)
- Modify: `internal/auth/google_test.go` (add tests after existing tests)

**Step 1: Write the failing test for a successful device code request**

Add to `internal/auth/google_test.go`:

```go
func TestRequestDeviceCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("client_id") != "client-id" {
			t.Fatalf("expected client_id=client-id, got %s", r.FormValue("client_id"))
		}
		if r.FormValue("scope") == "" {
			t.Fatal("expected non-empty scope")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-code-123",
			"user_code":        "ABCD-EFGH",
			"verification_url": "https://www.google.com/device",
			"expires_in":       300,
			"interval":         5,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		DeviceCodeURL: server.URL,
	}

	resp, err := provider.requestDeviceCode("gmail")
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceCode != "device-code-123" {
		t.Fatalf("expected device-code-123, got %s", resp.DeviceCode)
	}
	if resp.UserCode != "ABCD-EFGH" {
		t.Fatalf("expected ABCD-EFGH, got %s", resp.UserCode)
	}
	if resp.VerificationURL != "https://www.google.com/device" {
		t.Fatalf("expected verification URL, got %s", resp.VerificationURL)
	}
	if resp.ExpiresIn != 300 {
		t.Fatalf("expected 300, got %d", resp.ExpiresIn)
	}
	if resp.Interval != 5 {
		t.Fatalf("expected 5, got %d", resp.Interval)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestRequestDeviceCode -v`
Expected: FAIL -- `DeviceCodeURL` field and `requestDeviceCode` method don't exist yet.

**Step 3: Write the implementation**

Add to `internal/auth/google.go`:

1. Add a `DeviceCodeURL` field to `GoogleAuth`:

```go
type GoogleAuth struct {
	ClientID      string
	ClientSecret  string
	TokenURL      string
	DeviceCodeURL string
	Store         CredentialStore
}
```

2. Add the `deviceCodeResponse` struct and `requestDeviceCode` method after `refreshToken()`:

```go
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (g *GoogleAuth) requestDeviceCode(service string) (deviceCodeResponse, error) {
	scopes, ok := defaultScopes[service]
	if !ok {
		return deviceCodeResponse{}, fmt.Errorf("unknown service: %s", service)
	}

	deviceCodeURL := g.DeviceCodeURL
	if deviceCodeURL == "" {
		deviceCodeURL = "https://oauth2.googleapis.com/device/code"
	}

	params := url.Values{
		"client_id": {g.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	resp, err := http.PostForm(deviceCodeURL, params)
	if err != nil {
		return deviceCodeResponse{}, err
	}
	defer resp.Body.Close()

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return deviceCodeResponse{}, err
	}

	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestRequestDeviceCode -v`
Expected: PASS

**Step 5: Write a failing test for error response from device code endpoint**

Add to `internal/auth/google_test.go`:

```go
func TestRequestDeviceCodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "access_denied",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		DeviceCodeURL: server.URL,
	}

	resp, err := provider.requestDeviceCode("gmail")
	if err != nil {
		t.Fatal("expected no transport error")
	}
	// The response body was decoded but device_code will be empty.
	// The caller (runDeviceFlow) checks for empty device_code.
	if resp.DeviceCode != "" {
		t.Fatalf("expected empty device code, got %s", resp.DeviceCode)
	}
}

func TestRequestDeviceCodeUnknownService(t *testing.T) {
	provider := &GoogleAuth{ClientID: "client-id"}

	_, err := provider.requestDeviceCode("unknown-service")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestRequestDeviceCode -v`
Expected: PASS (all three tests)

**Step 7: Commit**

```bash
git add internal/auth/google.go internal/auth/google_test.go
git commit -m "feat(auth): add requestDeviceCode for Google device flow"
```

---

### Task 2: Add `pollDeviceToken()` with tests

**Files:**
- Modify: `internal/auth/google.go` (add method after `requestDeviceCode()`)
- Modify: `internal/auth/google_test.go` (add tests)

**Step 1: Write the failing test for successful polling**

Add to `internal/auth/google_test.go`:

```go
func TestPollDeviceTokenSuccess(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
		}
		if r.FormValue("device_code") != "device-code-123" {
			t.Fatalf("unexpected device_code: %s", r.FormValue("device_code"))
		}

		attempt++
		if attempt < 3 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "authorization_pending",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "device-access-token",
			"refresh_token": "device-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	token, refresh, expiry, err := provider.pollDeviceToken("device-code-123", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if token != "device-access-token" {
		t.Fatalf("expected device-access-token, got %s", token)
	}
	if refresh != "device-refresh-token" {
		t.Fatalf("expected device-refresh-token, got %s", refresh)
	}
	if expiry == "" {
		t.Fatal("expected non-empty expiry")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestPollDeviceToken -v`
Expected: FAIL -- `pollDeviceToken` method doesn't exist yet.

**Step 3: Write the implementation**

Add to `internal/auth/google.go` after `requestDeviceCode()`:

```go
func (g *GoogleAuth) pollDeviceToken(deviceCode string, interval, expiresIn int) (string, string, string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	if interval < 1 {
		interval = 5
	}

	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return "", "", "", fmt.Errorf("device flow timed out")
		}

		time.Sleep(time.Duration(interval) * time.Second)

		params := url.Values{
			"client_id":     {g.ClientID},
			"client_secret": {g.ClientSecret},
			"device_code":   {deviceCode},
			"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		}

		resp, err := http.PostForm(tokenURL, params)
		if err != nil {
			return "", "", "", err
		}

		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		switch result.Error {
		case "":
			expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
			return result.AccessToken, result.RefreshToken, expiry, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		case "access_denied":
			return "", "", "", fmt.Errorf("user denied access")
		default:
			return "", "", "", fmt.Errorf("device flow error: %s", result.Error)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestPollDeviceTokenSuccess -v`
Expected: PASS

**Step 5: Write a failing test for user denial**

Add to `internal/auth/google_test.go`:

```go
func TestPollDeviceTokenDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "access_denied",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	_, _, _, err := provider.pollDeviceToken("device-code-123", 1, 10)
	if err == nil {
		t.Fatal("expected error for access denied")
	}
	if err.Error() != "user denied access" {
		t.Fatalf("expected 'user denied access', got %q", err.Error())
	}
}
```

**Step 6: Write a failing test for slow_down handling**

Add to `internal/auth/google_test.go`:

```go
func TestPollDeviceTokenSlowDown(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "slow_down",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "token-after-slowdown",
			"refresh_token": "refresh-after-slowdown",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	token, _, _, err := provider.pollDeviceToken("device-code-123", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-after-slowdown" {
		t.Fatalf("expected token-after-slowdown, got %s", token)
	}
}
```

**Step 7: Run all poll tests**

Run: `go test ./internal/auth/ -run TestPollDeviceToken -v`
Expected: PASS (all three tests)

**Step 8: Commit**

```bash
git add internal/auth/google.go internal/auth/google_test.go
git commit -m "feat(auth): add pollDeviceToken for Google device flow"
```

---

### Task 3: Add `runDeviceFlow()` and wire into `GetToken()`

**Files:**
- Modify: `internal/auth/google.go` (add `runDeviceFlow()`, modify `GetToken()`)
- Modify: `internal/auth/google_test.go` (add integration-style test)

**Step 1: Write the failing test for GetToken falling back to device flow**

This test sets up two HTTP servers: one for refresh (always fails) and one for device code + polling (succeeds). It verifies that `GetToken()` transparently falls back to the device flow and returns a valid token.

Add to `internal/auth/google_test.go`:

```go
func TestGetTokenFallsBackToDeviceFlow(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.FormValue("grant_type")
		if grantType == "refresh_token" {
			json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_grant",
				"error_description": "Token has been expired or revoked.",
			})
			return
		}
		if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "device-flow-token",
				"refresh_token": "device-flow-refresh",
				"expires_in":    3600,
			})
			return
		}
		t.Fatalf("unexpected grant_type: %s", grantType)
	}))
	defer refreshServer.Close()

	deviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_url": "https://www.google.com/device",
			"expires_in":       30,
			"interval":         1,
		})
	}))
	defer deviceServer.Close()

	store := newFakeStore()
	store.Put("gmail:test@gmail.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenURL:      refreshServer.URL,
		DeviceCodeURL: deviceServer.URL,
		Store:         store,
	}

	token, err := provider.GetToken("gmail", "test@gmail.com")
	if err != nil {
		t.Fatalf("expected device flow fallback to succeed, got: %v", err)
	}
	if token != "device-flow-token" {
		t.Fatalf("expected device-flow-token, got %s", token)
	}

	updated, _ := store.Get("gmail:test@gmail.com")
	if updated.RefreshToken != "device-flow-refresh" {
		t.Fatal("store should have the new refresh token from device flow")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestGetTokenFallsBackToDeviceFlow -v`
Expected: FAIL -- `runDeviceFlow` doesn't exist yet, `GetToken()` returns `AuthExpiredError`.

**Step 3: Write `runDeviceFlow()` and modify `GetToken()`**

Add `runDeviceFlow()` to `internal/auth/google.go` after `pollDeviceToken()`:

```go
func (g *GoogleAuth) runDeviceFlow(service, account string) (string, error) {
	resp, err := g.requestDeviceCode(service)
	if err != nil {
		return "", err
	}
	if resp.DeviceCode == "" {
		return "", fmt.Errorf("empty device code in response")
	}

	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] Verification URL: %s\n", resp.VerificationURL)
	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] User Code: %s\n", resp.UserCode)
	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] Waiting for approval (expires in %ds)...\n", resp.ExpiresIn)

	token, refresh, expiry, err := g.pollDeviceToken(resp.DeviceCode, resp.Interval, resp.ExpiresIn)
	if err != nil {
		return "", err
	}

	key := service + ":" + account
	if err := g.Store.Put(key, credstore.Credential{
		AccessToken:  token,
		RefreshToken: refresh,
		Expiry:       expiry,
	}); err != nil {
		return "", fmt.Errorf("saving device flow token: %w", err)
	}

	return token, nil
}
```

Modify `GetToken()` -- replace the current `AuthExpiredError` return with a device flow fallback. Change lines 78-80 from:

```go
	if err != nil {
		return "", &AuthExpiredError{Service: service, Account: account, Err: err}
	}
```

To:

```go
	if err != nil {
		token, deviceErr := g.runDeviceFlow(service, account)
		if deviceErr != nil {
			return "", &AuthExpiredError{Service: service, Account: account, Err: fmt.Errorf("refresh failed: %w; device flow failed: %w", err, deviceErr)}
		}
		return token, nil
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestGetTokenFallsBackToDeviceFlow -v`
Expected: PASS

**Step 5: Write a test for device flow also failing (falls through to AuthExpiredError)**

Add to `internal/auth/google_test.go`:

```go
func TestGetTokenDeviceFlowAlsoFails(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Token has been expired or revoked.",
		})
	}))
	defer tokenServer.Close()

	deviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_url": "https://www.google.com/device",
			"expires_in":       2,
			"interval":         1,
		})
	}))
	defer deviceServer.Close()

	store := newFakeStore()
	store.Put("gmail:test@gmail.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenURL:      tokenServer.URL,
		DeviceCodeURL: deviceServer.URL,
		Store:         store,
	}

	_, err := provider.GetToken("gmail", "test@gmail.com")
	if err == nil {
		t.Fatal("expected AuthExpiredError when both refresh and device flow fail")
	}

	var authErr *AuthExpiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthExpiredError, got %T: %v", err, err)
	}
}
```

Note: this test also needs `"errors"` in the test file import list.

**Step 6: Run all auth tests to ensure nothing is broken**

Run: `go test ./internal/auth/ -v`
Expected: ALL PASS

**Step 7: Commit**

```bash
git add internal/auth/google.go internal/auth/google_test.go
git commit -m "feat(auth): wire device flow fallback into GetToken"
```

---

### Task 4: Update help text

**Files:**
- Modify: `cmd/kuma-approve/main.go:440-462` (the `printUsage` function)

**Step 1: Update the help text**

In `cmd/kuma-approve/main.go`, replace the `printUsage()` body with:

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

Token Re-Auth:
  When a Google token expires mid-operation, the CLI automatically starts the
  OAuth device authorization flow (RFC 8628). It prints a verification URL and
  user code to stderr with the [AUTH_DEVICE_FLOW] prefix, then polls until the
  user approves on any device. The original operation retries on success.

  Requires "TVs and Limited Input devices" enabled in Google Cloud Console.

Examples:
  kuma-approve gmail list --limit 10
  kuma-approve gcal create --title "Standup" --start 2026-09-05T09:00:00Z --end 2026-09-05T09:30:00Z
  kuma-approve exec run --cmd "df -h"

Run 'kuma-approve <service> --help' for actions, parameters, and examples.
Run 'kuma-approve setup' for first-time configuration.`)
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/kuma-approve/`
Expected: Builds without errors.

**Step 3: Run the full test suite**

Run: `go test ./...`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add cmd/kuma-approve/main.go
git commit -m "docs: add device flow re-auth section to help text"
```
