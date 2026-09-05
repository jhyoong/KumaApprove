# KumaApprove

A CLI tool for AI agents to securely interact with personal services (Gmail, Google Calendar, shell commands) with human-in-the-loop approval via Telegram. Compiles to a single Go binary with zero runtime dependencies.

Agents invoke KumaApprove as a subprocess, receive structured JSON on stdout, and move on. Write actions (sending emails, creating events, running commands) require explicit Telegram approval before execution.

## Prerequisites

- Go 1.21+
- A Telegram bot (create one via [@BotFather](https://t.me/botfather))
- A Google Cloud project with Gmail API and Calendar API enabled
- OAuth 2.0 client credentials (Desktop type) from Google Cloud Console
- Optional: a Microsoft Entra ID (Azure AD) app registration for Outlook and Microsoft Calendar

### Google OAuth setup

1. Create a Google Cloud project (free, no billing required).
2. Enable the Gmail API and Google Calendar API.
3. Create an OAuth 2.0 Client ID (Application type: Desktop).
4. Set the OAuth consent screen to "Production" (unverified). Users see a one-time "This app isn't verified" warning during initial auth. This avoids the 7-day refresh token expiry that "Testing" mode enforces.

### Microsoft OAuth setup (optional)

Only needed for the `outlook` and `msft-cal` services. Skip this and answer `n` to the
Microsoft section of the setup wizard to use Gmail/GCal only.

1. In the [Azure portal](https://portal.azure.com), go to **App registrations → New registration**.
2. Set **Supported account types** to "Accounts in any organizational directory and personal Microsoft accounts" if personal (outlook.com/hotmail.com) sign-in is required; choose "Single tenant" for a work account only. The setup wizard defaults the tenant to `consumers`, which targets personal accounts.
3. Under **Authentication → Mobile & desktop applications**, add the redirect URI `http://localhost:8400/callback`. No client secret is needed — the app runs as a public client.
4. Under **API permissions**, add the delegated permissions `Mail.Read`, `Mail.ReadWrite`, `Mail.Send`, `Calendars.Read`, `Calendars.ReadWrite`, and `offline_access`.

The redirect URI must match `http://localhost:8400/callback` **exactly** — scheme, host, port, and path. Microsoft, unlike Google, does not accept a loopback IP such as `127.0.0.1` and does not allow a wildcard port. A mismatch fails at the consent screen with `AADSTS50011` before any token is issued. If port 8400 is in use on your machine, change `msCallbackPort` in `internal/auth/microsoft.go` and register the matching URI.

## Installation

```bash
go install github.com/jhyoong/KumaApprove/cmd/kuma-approve@latest
```

Or build from source:

```bash
git clone https://github.com/jhyoong/KumaApprove.git
cd KumaApprove
go build -o kuma-approve ./cmd/kuma-approve/
```

## Setup

Run the interactive setup wizard:

```bash
kuma-approve setup
```

The wizard will:

1. Create `~/.kuma-approve/` and `~/.kuma-approve/workspace/`.
2. Prompt for your Telegram bot token.
3. Auto-detect your Telegram chat ID (send any message to the bot first).
4. Prompt for Google OAuth client ID and client secret.
5. Prompt for your Google account email.
6. Open a browser for Gmail OAuth authorization.
7. Open a browser for Google Calendar OAuth authorization.
8. Send a test Telegram message to confirm the bot works.

## Usage

```
kuma-approve <service> <action> [flags]
```

### Gmail

```bash
# List recent messages
kuma-approve gmail list --limit 20

# Get a message by ID
kuma-approve gmail get --id <message_id>

# Search messages
kuma-approve gmail search --query "from:boss subject:urgent"

# Send an email (requires Telegram approval)
kuma-approve gmail send --to "user@example.com" --subject "Hello" --body "Message text"

# Reply to a message (requires Telegram approval)
kuma-approve gmail reply --id <message_id> --body "Reply text"

# Create a draft
kuma-approve gmail draft --to "user@example.com" --subject "Draft" --body "Draft text"
```

### Google Calendar

```bash
# List today's events
kuma-approve gcal list

# List events for a specific date
kuma-approve gcal list --date 2026-09-04

# Get event details
kuma-approve gcal get --event-id <id>

# Create an event (requires Telegram approval)
kuma-approve gcal create --title "Meeting" --start "2026-09-04T10:00:00Z" --end "2026-09-04T11:00:00Z"

# Update an event (requires Telegram approval)
kuma-approve gcal update --event-id <id> --title "New Title"

# Delete an event (requires Telegram approval)
kuma-approve gcal delete --event-id <id>
```

### Command Execution

```bash
# Run a command (safe-listed commands auto-approve)
kuma-approve exec run --cmd "df -h"

# Shell mode for pipes and redirects (always requires approval)
kuma-approve exec run --cmd "cat log.txt | grep ERROR" --shell true
```

### Authentication

```bash
# Re-authorize a service for an account
kuma-approve auth gmail user@gmail.com
kuma-approve auth gcal user@gmail.com
kuma-approve auth outlook you@outlook.com
kuma-approve auth msft-cal you@outlook.com
```

### Multi-account

If multiple accounts are configured for a service, use `--account`:

```bash
kuma-approve gmail list --account personal@gmail.com
kuma-approve gmail list --account work@gmail.com
```

If only one account exists for a service, `--account` is optional.

## Approval Tiers

Every action has a default approval tier that can be overridden in the config file.

| Tier | Behaviour | Examples |
|---|---|---|
| **auto** | Executes immediately | Read emails, list calendar events, safe-listed shell commands |
| **approve** | Blocks until Telegram approval or timeout | Send email, create/modify/delete events, non-safe-listed commands |
| **deny** | Rejected outright | Commands matching deny-list patterns (`sudo`, `rm -rf`, etc.) |

Using `--shell true` automatically bumps any command to the "approve" tier.

## Output Format

All output is JSON to stdout:

```json
{
  "success": true,
  "action": "gmail:list",
  "data": { },
  "error": null
}
```

Errors:

```json
{
  "success": false,
  "action": "gmail:send",
  "data": null,
  "error": {
    "code": "APPROVAL_REJECTED",
    "message": "Action was rejected by user via Telegram"
  }
}
```

Error codes: `APPROVAL_REJECTED`, `APPROVAL_TIMEOUT`, `AUTH_EXPIRED`, `DENIED_BY_POLICY`, `EXECUTION_TIMEOUT`, `API_ERROR`, `INVALID_ARGS`.

## Configuration

Config file: `~/.kuma-approve/config.json`

```json
{
  "telegram": {
    "bot_token": "...",
    "chat_id": "..."
  },
  "approval": {
    "timeout_minutes": 240,
    "tiers": {
      "gmail:list": "auto",
      "gmail:get": "auto",
      "gmail:search": "auto",
      "gmail:send": "approve",
      "gmail:reply": "approve",
      "gmail:draft": "auto",
      "gcal:list": "auto",
      "gcal:get": "auto",
      "gcal:create": "approve",
      "gcal:update": "approve",
      "gcal:delete": "approve",
      "exec:run": "approve"
    }
  },
  "exec": {
    "working_directory": "~/.kuma-approve/workspace",
    "timeout_seconds": 60,
    "max_output_bytes": 102400,
    "safe_list": ["ls", "cat", "df", "date", "echo", "pwd", "wc", "head", "tail", "grep", "find"],
    "deny_list_patterns": ["^sudo\\b", "^rm\\s+-rf", "^shutdown", "^reboot", "^mkfs", "^dd\\b"]
  },
  "google_oauth": {
    "client_id": "...",
    "client_secret": "..."
  },
  "microsoft_oauth": {
    "client_id": "...",
    "tenant_id": "consumers"
  },
  "accounts": {
    "gmail": ["personal@gmail.com"],
    "gcal": ["personal@gmail.com"],
    "outlook": ["you@outlook.com"],
    "msft-cal": ["you@outlook.com"]
  }
}
```

`microsoft_oauth` and the `outlook`/`msft-cal` account entries are optional. Omit them (or leave `microsoft_oauth.client_id` empty) and the Outlook/Calendar services are not registered, so `kuma-approve outlook ...` fails with `unknown service`. `tenant_id` defaults to `consumers` (personal Microsoft accounts); use `common` or a specific tenant GUID for work/school accounts.

Override any action's tier in the `tiers` map. Add commands to `safe_list` to auto-approve them, or add regex patterns to `deny_list_patterns` to block them entirely.

## Docker / Container Usage

KumaApprove can run inside a Docker container (for example, as a tool available to an AI agent). Two things need special handling: OAuth callbacks and credential encryption.

### OAuth callback ports

During `kuma-approve setup`, Google and Microsoft OAuth flows start a temporary HTTP server on `localhost` and wait for the browser to redirect back with an authorization code.

- **Google** normally picks a random port. Inside a container, set the `KUMA_OAUTH_PORT` environment variable to a fixed port number (e.g., `9401`) so Docker can map it to the host. The callback listener binds to `0.0.0.0` when this variable is set.
- **Microsoft** always uses the fixed port defined in `internal/auth/microsoft.go` (default `8400`). When `KUMA_OAUTH_PORT` is set, the listener binds to `0.0.0.0` instead of loopback so that Docker port mapping works. The redirect URI registered in Azure AD (`http://localhost:8400/callback`) does not change.

Both ports must be published to the host so the browser redirect can reach the container.

### Machine-bound credentials

Credentials are encrypted using the machine's unique ID (see [Credential Storage](#credential-storage) below). A Docker container has a different machine ID from the host, so you cannot share a host's `credentials.enc` directly.

To keep credentials stable across container recreations, generate a fixed machine-id file once and mount it at `/etc/machine-id`:

```bash
uuidgen | tr '[:upper:]' '[:lower:]' | tr -d '-' > kuma-machine-id
```

### Example Docker Compose setup

```yaml
services:
  my-agent:
    # ... your agent config ...
    volumes:
      - ./kuma-approve-linux:/usr/local/bin/kuma-approve:ro
      - ./kuma-machine-id:/etc/machine-id:ro
      - ~/.kuma-approve-docker:/home/agent/.kuma-approve
    environment:
      KUMA_OAUTH_PORT: "9401"
    ports:
      - 127.0.0.1:9401:9401   # Google OAuth callback
      - 127.0.0.1:8400:8400   # Microsoft OAuth callback
```

Cross-compile the binary for Linux before mounting:

```bash
GOOS=linux GOARCH=amd64 go build -o kuma-approve-linux ./cmd/kuma-approve/
```

Then run setup inside the container once:

```bash
docker compose exec my-agent kuma-approve setup
```

Open the OAuth URLs printed by setup in your host browser. The callbacks will redirect to `localhost` and reach the container through the port mappings.

## Credential Storage

Credentials are stored encrypted at `~/.kuma-approve/credentials.enc` using AES-256-GCM. The encryption key is derived via Argon2id from the machine's unique identifier (`/etc/machine-id` on Linux, `IOPlatformUUID` on macOS).

**Important: credentials are machine-bound.** If you move to a different machine, the credential file becomes unreadable. Run `kuma-approve setup` again to re-authenticate on the new machine.

## Token Refresh

Access tokens are refreshed automatically before each API call when they are near expiry. If a refresh fails (e.g. the token has been revoked), the CLI returns an `AUTH_EXPIRED` error and sends a Telegram notification with re-authorization instructions.

## Audit Log

Every CLI invocation is logged to `~/.kuma-approve/audit.log` as newline-delimited JSON. Each entry records the timestamp, action, account, parameters (sanitised), approval status, result, and error code. Email bodies are truncated to 200 characters in the log.
