# Targeted Setup Wizard Design

## Problem

The current `kuma-approve setup` wizard is linear. Any issue or change requires re-running the full setup, re-entering all credentials, and re-authorizing all OAuth flows -- even if only one section needs attention.

## Solution

Make `Run()` detect existing configuration and credential validity, then let the user skip sections that are already working.

## Setup Sections

The wizard has three sections, evaluated in order:

1. **Telegram** -- bot token + chat ID
2. **Google** -- OAuth client credentials + Gmail/GCal account + OAuth flows
3. **Microsoft** (optional) -- OAuth client credentials + Outlook/msft-cal account + OAuth flows

## Validation

Each section has a validation function that returns a `sectionStatus`:

```go
type sectionStatus struct {
    configured bool   // config fields are non-empty
    valid      bool   // credentials verified via network
    reason     string // human-readable explanation when invalid
    detail     string // context for skip prompt (e.g. "chat ID: 12345")
}
```

| Function | Config check | Network validation |
|---|---|---|
| `validateTelegram(cfg)` | `telegram.bot_token` non-empty | `GET /bot<token>/getMe` returns `ok: true` |
| `validateGoogle(cfg, store)` | `google_oauth.client_id` non-empty + accounts exist | `GoogleAuth.GetToken()` for each gmail/gcal account |
| `validateMicrosoft(cfg, store)` | `microsoft_oauth.client_id` non-empty + accounts exist | `MicrosoftAuth.GetToken()` for each outlook/msft-cal account |

Network validation failures (timeout, DNS) are treated as "configured but invalid."

## UX Flow

```
=== KumaApprove Setup Wizard ===

Checking existing configuration...

[1] Telegram
    Status: configured and valid (chat ID: 12345)
    [S]kip / [R]econfigure? _

[2] Google (Gmail + Calendar)
    Status: configured but Gmail token expired
    Reconfiguring...
    Google OAuth client ID: _

[3] Microsoft (Outlook + Calendar)
    Status: not configured
    Configure Microsoft Outlook/Calendar? (y/n): _
```

Rules per section:

- **Valid**: show `[S]kip / [R]econfigure?` prompt. Skip preserves config and credentials.
- **Configured but invalid**: print reason, auto-enter setup (no prompt).
- **Not configured**: enter setup directly (Telegram/Google) or ask `Configure? (y/n)` (Microsoft).

## Data Flow

1. Load existing config via `config.Load()`. Fall back to `config.Default()` if file missing.
2. Open credential store. Create empty one if it does not exist.
3. Run validators for each section.
4. Skipped sections: config and credentials carry forward unchanged.
5. Reconfigured sections: overwrite config fields, re-run OAuth flows.
6. `config.Save()` after each section completes (incremental, not batched).

Orphaned credentials from changed account emails are left in the store (harmless, never looked up).

## Error Handling

- Network validation failure -> treat as invalid, auto-enter setup.
- Corrupted credential store -> warn, treat all sections as not configured, recreate store.
- OAuth flow failure during setup -> exit with error (same as today). Earlier sections are preserved by incremental save.

## Testing

- Three `validate*` functions are independently testable using the existing `CredentialStore` interface with fake stores.
- Prompt/skip logic uses stdin (same as today, same coverage level).

## Files Changed

- `internal/setup/setup.go` -- rewrite `Run()`, add `sectionStatus`, three `validate*` functions, `validateTelegramBot()` helper
- `internal/setup/setup_test.go` -- add tests for validation functions
