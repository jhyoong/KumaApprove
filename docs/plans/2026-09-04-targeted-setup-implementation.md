# Targeted Setup Wizard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `kuma-approve setup` detect existing configuration, validate credentials over the network, and let the user skip or reconfigure individual sections.

**Architecture:** Three validation functions (`validateTelegram`, `validateGoogle`, `validateMicrosoft`) return a `sectionStatus` struct. `Run()` becomes a loop that checks each section's status and either skips, auto-enters, or prompts. Config is saved incrementally after each section. Validation functions accept injectable URLs/stores for testability.

**Tech Stack:** Go stdlib only. Uses existing `auth.CredentialStore` interface and `auth.GoogleAuth`/`auth.MicrosoftAuth` types. Telegram validation uses `net/http` + `net/http/httptest`.

---

### Task 1: Add `sectionStatus` type and `validateTelegramBot` helper

**Files:**
- Modify: `internal/setup/setup.go`
- Modify: `internal/setup/setup_test.go`

**Step 1: Write the failing test for `validateTelegramBot`**

Add to `internal/setup/setup_test.go`:

```go
package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateTelegramBotValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":       123,
				"is_bot":   true,
				"username": "test_bot",
			},
		})
	}))
	defer server.Close()

	err := validateTelegramBot("fake-token", server.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateTelegramBotInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Unauthorized",
		})
	}))
	defer server.Close()

	err := validateTelegramBot("bad-token", server.URL)
	if err == nil {
		t.Fatal("expected error for invalid bot token")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -run TestValidateTelegramBot -v`
Expected: FAIL with "undefined: validateTelegramBot"

**Step 3: Write minimal implementation**

Add to `internal/setup/setup.go`:

```go
type sectionStatus struct {
	configured bool
	valid      bool
	reason     string
	detail     string
}

func validateTelegramBot(botToken, apiBase string) error {
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	url := fmt.Sprintf("%s/bot%s/getMe", apiBase, botToken)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("connecting to Telegram API: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parsing Telegram response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("bot token invalid: %s", result.Description)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -run TestValidateTelegramBot -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): add sectionStatus type and validateTelegramBot helper"
```

---

### Task 2: Add `validateTelegram` function

**Files:**
- Modify: `internal/setup/setup.go`
- Modify: `internal/setup/setup_test.go`

**Step 1: Write the failing tests**

Add to `internal/setup/setup_test.go`:

```go
import (
	"github.com/jhyoong/KumaApprove/internal/config"
)

func TestValidateTelegramNotConfigured(t *testing.T) {
	cfg := config.Config{}
	status := validateTelegram(cfg, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateTelegramValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": 1}})
	}))
	defer server.Close()

	cfg := config.Config{
		Telegram: config.TelegramConfig{BotToken: "test-token", ChatID: "12345"},
	}
	status := validateTelegram(cfg, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
	if status.detail != "chat ID: 12345" {
		t.Fatalf("expected detail 'chat ID: 12345', got %q", status.detail)
	}
}

func TestValidateTelegramInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	defer server.Close()

	cfg := config.Config{
		Telegram: config.TelegramConfig{BotToken: "bad-token", ChatID: "12345"},
	}
	status := validateTelegram(cfg, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
	if status.reason == "" {
		t.Fatal("expected a reason")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -run TestValidateTelegram -v`
Expected: FAIL with "undefined: validateTelegram"

**Step 3: Write minimal implementation**

Add to `internal/setup/setup.go`:

```go
func validateTelegram(cfg config.Config, apiBase string) sectionStatus {
	if cfg.Telegram.BotToken == "" {
		return sectionStatus{configured: false}
	}
	detail := "chat ID: " + cfg.Telegram.ChatID
	if err := validateTelegramBot(cfg.Telegram.BotToken, apiBase); err != nil {
		return sectionStatus{configured: true, valid: false, reason: err.Error(), detail: detail}
	}
	return sectionStatus{configured: true, valid: true, detail: detail}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -run TestValidateTelegram -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): add validateTelegram function"
```

---

### Task 3: Add `validateGoogle` function

**Files:**
- Modify: `internal/setup/setup.go`
- Modify: `internal/setup/setup_test.go`

**Step 1: Write the failing tests**

The setup package needs to import `auth` to construct a `GoogleAuth` for validation. Tests use a fake store (same pattern as `internal/auth/google_test.go`) and an httptest token server.

Add to `internal/setup/setup_test.go`:

```go
import (
	"fmt"
	"time"

	"github.com/jhyoong/KumaApprove/internal/auth"
	"github.com/jhyoong/KumaApprove/internal/credstore"
)

type fakeStore struct {
	creds map[string]credstore.Credential
}

func newFakeStore() *fakeStore {
	return &fakeStore{creds: make(map[string]credstore.Credential)}
}

func (f *fakeStore) Get(key string) (credstore.Credential, error) {
	c, ok := f.creds[key]
	if !ok {
		return credstore.Credential{}, fmt.Errorf("not found: %s", key)
	}
	return c, nil
}

func (f *fakeStore) Put(key string, c credstore.Credential) error {
	f.creds[key] = c
	return nil
}

func TestValidateGoogleNotConfigured(t *testing.T) {
	cfg := config.Config{}
	store := newFakeStore()
	status := validateGoogle(cfg, store, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateGoogleValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("gmail:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
	store.Put("gcal:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}, "gcal": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
}

func TestValidateGoogleExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Token has been revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("gmail:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "bad-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}, "gcal": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
}

func TestValidateGoogleNoCredentials(t *testing.T) {
	store := newFakeStore()
	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, "")
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid when no credentials in store")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -run TestValidateGoogle -v`
Expected: FAIL with "undefined: validateGoogle"

**Step 3: Write minimal implementation**

Add to `internal/setup/setup.go`:

```go
func validateGoogle(cfg config.Config, store auth.CredentialStore, tokenURL string) sectionStatus {
	if cfg.GoogleOAuth.ClientID == "" {
		return sectionStatus{configured: false}
	}

	gmailAccounts := cfg.Accounts["gmail"]
	gcalAccounts := cfg.Accounts["gcal"]
	if len(gmailAccounts) == 0 && len(gcalAccounts) == 0 {
		return sectionStatus{configured: false}
	}

	gauth := &auth.GoogleAuth{
		ClientID:     cfg.GoogleOAuth.ClientID,
		ClientSecret: cfg.GoogleOAuth.ClientSecret,
		TokenURL:     tokenURL,
		Store:        store,
	}

	var account string
	if len(gmailAccounts) > 0 {
		account = gmailAccounts[0]
	} else {
		account = gcalAccounts[0]
	}
	detail := "account: " + account

	for _, acct := range gmailAccounts {
		if _, err := gauth.GetToken("gmail", acct); err != nil {
			return sectionStatus{configured: true, valid: false, reason: fmt.Sprintf("Gmail token invalid for %s: %v", acct, err), detail: detail}
		}
	}
	for _, acct := range gcalAccounts {
		if _, err := gauth.GetToken("gcal", acct); err != nil {
			return sectionStatus{configured: true, valid: false, reason: fmt.Sprintf("GCal token invalid for %s: %v", acct, err), detail: detail}
		}
	}

	return sectionStatus{configured: true, valid: true, detail: detail}
}
```

Note: add `"github.com/jhyoong/KumaApprove/internal/auth"` to imports in `setup.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -run TestValidateGoogle -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): add validateGoogle function"
```

---

### Task 4: Add `validateMicrosoft` function

**Files:**
- Modify: `internal/setup/setup.go`
- Modify: `internal/setup/setup_test.go`

**Step 1: Write the failing tests**

Add to `internal/setup/setup_test.go`:

```go
func TestValidateMicrosoftNotConfigured(t *testing.T) {
	cfg := config.Config{}
	store := newFakeStore()
	status := validateMicrosoft(cfg, store, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateMicrosoftValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-token",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
	store.Put("msft-cal:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		MicrosoftOAuth: config.MicrosoftOAuthConfig{ClientID: "cid", ClientSecret: "csec", TenantID: "consumers"},
		Accounts:       map[string][]string{"outlook": {"user@outlook.com"}, "msft-cal": {"user@outlook.com"}},
	}
	status := validateMicrosoft(cfg, store, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
}

func TestValidateMicrosoftExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "token revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "bad-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		MicrosoftOAuth: config.MicrosoftOAuthConfig{ClientID: "cid", ClientSecret: "csec", TenantID: "consumers"},
		Accounts:       map[string][]string{"outlook": {"user@outlook.com"}},
	}
	status := validateMicrosoft(cfg, store, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -run TestValidateMicrosoft -v`
Expected: FAIL with "undefined: validateMicrosoft"

**Step 3: Write minimal implementation**

Add to `internal/setup/setup.go`:

```go
func validateMicrosoft(cfg config.Config, store auth.CredentialStore, tokenURL string) sectionStatus {
	if cfg.MicrosoftOAuth.ClientID == "" {
		return sectionStatus{configured: false}
	}

	outlookAccounts := cfg.Accounts["outlook"]
	msftCalAccounts := cfg.Accounts["msft-cal"]
	if len(outlookAccounts) == 0 && len(msftCalAccounts) == 0 {
		return sectionStatus{configured: false}
	}

	msauth := &auth.MicrosoftAuth{
		ClientID:     cfg.MicrosoftOAuth.ClientID,
		ClientSecret: cfg.MicrosoftOAuth.ClientSecret,
		TenantID:     cfg.MicrosoftOAuth.TenantID,
		TokenURL:     tokenURL,
		Store:        store,
	}

	var account string
	if len(outlookAccounts) > 0 {
		account = outlookAccounts[0]
	} else {
		account = msftCalAccounts[0]
	}
	detail := "account: " + account

	for _, acct := range outlookAccounts {
		if _, err := msauth.GetToken("outlook", acct); err != nil {
			return sectionStatus{configured: true, valid: false, reason: fmt.Sprintf("Outlook token invalid for %s: %v", acct, err), detail: detail}
		}
	}
	for _, acct := range msftCalAccounts {
		if _, err := msauth.GetToken("msft-cal", acct); err != nil {
			return sectionStatus{configured: true, valid: false, reason: fmt.Sprintf("msft-cal token invalid for %s: %v", acct, err), detail: detail}
		}
	}

	return sectionStatus{configured: true, valid: true, detail: detail}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -run TestValidateMicrosoft -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): add validateMicrosoft function"
```

---

### Task 5: Rewrite `Run()` to use section-based flow

**Files:**
- Modify: `internal/setup/setup.go`

This is the largest task. It replaces the linear `Run()` with the new section-based flow.

**Step 1: Add `promptSection` helper**

This helper prints the section status and returns whether to enter setup for that section.

```go
func promptSection(reader *bufio.Reader, name string, status sectionStatus, optional bool) bool {
	fmt.Printf("[%s]\n", name)
	if !status.configured {
		if optional {
			answer := readLine(reader, fmt.Sprintf("    Not configured. Configure %s? (y/n): ", name))
			return strings.ToLower(answer) == "y"
		}
		fmt.Println("    Not configured.")
		return true
	}
	if !status.valid {
		fmt.Printf("    Status: configured but invalid (%s)\n", status.reason)
		fmt.Println("    Reconfiguring...")
		return true
	}
	fmt.Printf("    Status: configured and valid (%s)\n", status.detail)
	answer := readLine(reader, "    [S]kip / [R]econfigure? ")
	return strings.ToLower(answer) == "r"
}
```

**Step 2: Rewrite `Run()`**

Replace the entire `Run()` function body. The new version:

1. Creates config dir + workspace (same as before).
2. Loads existing config or falls back to `config.Default()`.
3. Opens credential store (or creates empty one). If store is corrupted, warns and continues with nil store (all sections treated as not configured).
4. Runs validators.
5. For each section: calls `promptSection`, enters setup if needed, saves config incrementally.
6. Sends Telegram test message only if Telegram was reconfigured.

```go
func Run() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=== KumaApprove Setup Wizard ===")
	fmt.Println()

	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create config directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(config.Dir(), "workspace"), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create workspace directory: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	if cfg.Accounts == nil {
		cfg.Accounts = map[string][]string{}
	}

	machineID, err := credstore.GetMachineID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get machine ID: %v\n", err)
		os.Exit(1)
	}
	encKey, err := credstore.DeriveKey(machineID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to derive key: %v\n", err)
		os.Exit(1)
	}
	storePath := filepath.Join(config.Dir(), "credentials.enc")
	store, err := credstore.NewStore(storePath, encKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: credential store corrupted, will recreate: %v\n", err)
		os.Remove(storePath)
		store, err = credstore.NewStore(storePath, encKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create credential store: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Checking existing configuration...")
	fmt.Println()

	tgStatus := validateTelegram(cfg, "")
	gStatus := validateGoogle(cfg, store, "")
	msStatus := validateMicrosoft(cfg, store, "")

	// Section 1: Telegram
	telegramChanged := false
	if promptSection(reader, "Telegram", tgStatus, false) {
		botToken := readLine(reader, "    Telegram bot token: ")
		cfg.Telegram.BotToken = botToken

		chatID, err := detectChatID(botToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to detect chat ID: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("    Detected chat ID: %s\n", chatID)
		cfg.Telegram.ChatID = chatID
		telegramChanged = true

		if err := config.Save(cfg, config.DefaultPath()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("    Telegram configured.")
	}
	fmt.Println()

	// Section 2: Google
	if promptSection(reader, "Google (Gmail + Calendar)", gStatus, false) {
		clientID := readLine(reader, "    Google OAuth client ID: ")
		clientSecret := readLine(reader, "    Google OAuth client secret: ")
		cfg.GoogleOAuth.ClientID = clientID
		cfg.GoogleOAuth.ClientSecret = clientSecret

		account := readLine(reader, "    Google account email: ")
		cfg.Accounts["gmail"] = []string{account}
		cfg.Accounts["gcal"] = []string{account}

		if err := config.Save(cfg, config.DefaultPath()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
			os.Exit(1)
		}

		gauth := &auth.GoogleAuth{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Store:        store,
		}

		fmt.Println("    Authorizing Gmail...")
		if err := gauth.RunOAuthFlow("gmail", account); err != nil {
			fmt.Fprintf(os.Stderr, "Gmail OAuth failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("    Authorizing Google Calendar...")
		if err := gauth.RunOAuthFlow("gcal", account); err != nil {
			fmt.Fprintf(os.Stderr, "Google Calendar OAuth failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("    Google configured.")
	}
	fmt.Println()

	// Section 3: Microsoft (optional)
	if promptSection(reader, "Microsoft (Outlook + Calendar)", msStatus, true) {
		msClientID := readLine(reader, "    Microsoft Azure AD client ID: ")
		msClientSecret := readLine(reader, "    Microsoft Azure AD client secret: ")
		msTenantID := readLine(reader, "    Microsoft tenant ID (press Enter for 'consumers'): ")
		if msTenantID == "" {
			msTenantID = "consumers"
		}

		cfg.MicrosoftOAuth = config.MicrosoftOAuthConfig{
			ClientID:     msClientID,
			ClientSecret: msClientSecret,
			TenantID:     msTenantID,
		}

		msAccount := readLine(reader, "    Microsoft account email: ")
		cfg.Accounts["outlook"] = []string{msAccount}
		cfg.Accounts["msft-cal"] = []string{msAccount}

		if err := config.Save(cfg, config.DefaultPath()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
			os.Exit(1)
		}

		msauth := &auth.MicrosoftAuth{
			ClientID:     msClientID,
			ClientSecret: msClientSecret,
			TenantID:     msTenantID,
			Store:        store,
		}

		fmt.Println("    Authorizing Outlook...")
		if err := msauth.RunOAuthFlow("outlook", msAccount); err != nil {
			fmt.Fprintf(os.Stderr, "Outlook OAuth failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("    Authorizing Microsoft Calendar...")
		if err := msauth.RunOAuthFlow("msft-cal", msAccount); err != nil {
			fmt.Fprintf(os.Stderr, "Microsoft Calendar OAuth failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("    Microsoft configured.")
	}
	fmt.Println()

	// Send Telegram test message only if Telegram was reconfigured.
	if telegramChanged {
		if err := sendTestMessage(cfg.Telegram.BotToken, cfg.Telegram.ChatID); err != nil {
			fmt.Fprintf(os.Stderr, "failed to send test message: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Setup complete! KumaApprove is ready to use.")
}
```

**Step 3: Run all tests and build**

Run: `go test ./internal/setup/ -v`
Expected: All tests PASS

Run: `go vet ./...`
Expected: Clean

Run: `go build -o kuma-approve ./cmd/kuma-approve/`
Expected: Builds successfully

**Step 4: Commit**

```bash
git add internal/setup/setup.go
git commit -m "feat(setup): rewrite Run() with section-based validation and skip/reconfigure flow"
```

---

### Task 6: Final verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS across all packages

**Step 2: Run go vet**

Run: `go vet ./...`
Expected: Clean

**Step 3: Build**

Run: `go build -o kuma-approve ./cmd/kuma-approve/`
Expected: Builds successfully

**Step 4: Commit if any final fixes were needed**
