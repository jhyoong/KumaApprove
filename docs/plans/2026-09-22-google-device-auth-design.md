# Google Device Authorization Flow for Automatic Re-Auth

## Problem

KumaApprove's Google OAuth tokens expire every 7 days because the Google Cloud project is in "testing" mode. The current re-auth flow requires the user to be at the work machine with a terminal and browser to run `kuma-approve auth <service> <account>`, which starts a localhost callback server. When the user is away from the machine and an AI agent (e.g. Claude Code) hits `AUTH_EXPIRED`, there is no way to re-authorize without manual terminal access.

## Solution

Use Google's OAuth 2.0 Device Authorization Grant (RFC 8628) as an automatic fallback when token refresh fails. The device flow does not require a browser or localhost on the work machine -- the user approves from any device (phone, tablet, etc.) by visiting a URL and entering a short code.

## Design

### Integration point

The change is contained within `internal/auth/google.go`. The current `GetToken()` path:

```
GetToken() -> token expired? -> refreshToken() -> fails? -> return AuthExpiredError
```

Becomes:

```
GetToken() -> token expired? -> refreshToken() -> fails? -> runDeviceFlow() -> succeeds? -> store tokens, return access token
                                                                            -> fails/timeout? -> return AuthExpiredError
```

No changes to the router, services, config, credstore, or any callers.

### Device flow steps

**Step 1: Request device code.** POST to `https://oauth2.googleapis.com/device/code` with `client_id` and `scope`. Google returns `device_code`, `user_code`, `verification_url`, `expires_in`, and `interval`.

**Step 2: Emit prompt to stderr.** Print well-defined status lines:

```
[AUTH_DEVICE_FLOW] Verification URL: https://www.google.com/device
[AUTH_DEVICE_FLOW] User Code: ABCD-EFGH
[AUTH_DEVICE_FLOW] Waiting for approval (expires in 300s)...
```

The `[AUTH_DEVICE_FLOW]` prefix is a stable tag any AI agent or script can grep for in stderr. The verification URL and user code are on separate, predictable lines.

**Step 3: Poll for approval.** POST to `https://oauth2.googleapis.com/token` with `client_id`, `client_secret`, `device_code`, and `grant_type=urn:ietf:params:oauth:grant-type:device_code` at the interval Google specifies. Handle responses:

- `authorization_pending` -- keep polling
- `slow_down` -- increase interval by 5s
- `access_denied` -- user rejected, stop
- Success -- returns `access_token`, `refresh_token`, `expires_in`

On success, tokens are stored via `Store.Put()` and the access token is returned to `GetToken()`.

### Scopes

Reuses the existing `defaultScopes` map. The device flow requests the same scopes as the browser flow for the given service.

### Timeout

Respects Google's `expires_in` from step 1. If polling times out, falls back to `AuthExpiredError`.

### Error cases

All failure paths produce `AuthExpiredError`, which the router already handles:

- User denies on their device
- Codes expire before user acts
- Network error during polling

### Output contract

- **stdout** remains unchanged -- the JSON envelope is produced only after the operation completes (or fails).
- **stderr** carries the `[AUTH_DEVICE_FLOW]` lines during the device flow. If the flow succeeds, the original operation proceeds normally. If it fails, stdout gets the standard `AUTH_EXPIRED` error envelope.

## Files modified

- `internal/auth/google.go` -- Add `runDeviceFlow()`, `requestDeviceCode()`, `pollDeviceToken()` private methods. Modify `GetToken()` to call `runDeviceFlow()` when refresh fails.
- `internal/auth/google_test.go` -- New tests for device flow methods (mock HTTP endpoints).
- `cmd/kuma-approve/main.go` -- Update `printUsage()` to document the device flow behavior.

## Files unchanged

- `internal/cli/router.go` -- `AUTH_EXPIRED` handling stays the same; it just fires less often.
- `internal/setup/setup.go` -- Initial setup still uses the browser flow.
- `internal/credstore/` -- Device flow stores credentials the same way.
- `internal/config/` -- No new config fields.

## One-time user setup

Enable "TVs and Limited Input devices" as an OAuth client type in the Google Cloud Console for the project.
