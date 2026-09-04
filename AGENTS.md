# AGENTS.md — KumaApprove

Single-module Go CLI (`github.com/jhyoong/KumaApprove`, Go 1.26.4). Compiles to one static binary; only non-stdlib deps are Google API clients + `golang.org/x/crypto`. No Makefile, CI, linter config, or `opencode.json`.

## Commands

```bash
go build -o kuma-approve ./cmd/kuma-approve/
go test ./...
go test ./internal/<pkg>/ -run TestName -v   # single test, e.g. ./internal/approval/
go vet ./...
```

No lint/formatter config — keep `gofmt`-clean and match surrounding code. Do not commit the built `kuma-approve` binary at repo root (untracked build artifact).

## Entrypoint and dispatch

- `cmd/kuma-approve/main.go` is the only entrypoint. Flag parsing is hand-rolled: `--key value` pairs, bare `--flag` means `"true"`. There is no cobra/flag library — keep it that way.
- Dispatch shape: `kuma-approve setup | auth <service> <account> | <service> <action> [flags]`. `setup`/`auth` bypass the registry and call `internal/setup` directly.
- Services registered in `main.go`: `gmail`, `gcal` (both need `--account` resolution), `exec` (no account). `resolveAccount` strips `--account` before `Execute()` — service code never sees it. Single configured account is optional; multiple accounts without `--account` is a `NO_ACCOUNT` error.

## Architecture (`internal/`)

`config` → `credstore` → `auth` → `service/{gmail,gcal}` + `executor` → `approval` → `cli/router` → `output`, with `audit` + `setup` on the side.

- New service = new package under `internal/service/` implementing `service.Service` (`Name`/`Actions`/`Execute` in `internal/service/types.go`) + one `registry.Register()` call in `main.go`. Nothing else changes.
- `cli/router.go` owns the pipeline: tier resolve → Telegram approval → `Execute()` → audit log. Error-code mapping lives there (`AUTH_EXPIRED`, `EXECUTION_TIMEOUT`, else `API_ERROR`).
- Approval tiers (`internal/approval/tier.go`): `auto` / `approve` / `deny`. Defaults in `internal/config/config.go`; user overrides in `~/.kuma-approve/config.json` under `approval.tiers` keyed as `"service:action"`. `exec` with `--shell` always bumps to `approve`; deny-list regex matches reject outright with no approval path.
- Executor (`internal/executor/`): direct `exec.Command(binary, args...)` by default; `--shell` switches to `sh -c`. Env is stripped to a minimal set, working dir defaults to `~/.kuma-approve/workspace`, timeout default 60s, stdout/stderr truncated at `max_output_bytes`.

## Contracts to preserve

- Output: all results go through `internal/output` JSON envelope `{success, action, data, error{code,message}}` on stdout; failures exit nonzero. Never print prose to stdout or change envelope keys.
- Runtime state (never commit, never read real user files in tests): `~/.kuma-approve/{config.json, credentials.enc, audit.log, workspace/}`.
- Credentials are machine-bound (AES-256-GCM + Argon2id over `/etc/machine-id` on Linux / `IOPlatformUUID` on macOS). Moving machines invalidates `credentials.enc` — fix is re-running `kuma-approve setup`, not debugging crypto. Tests use temp dirs; never touch the real store.
- Audit log is append-only NDJSON; every `Dispatch` logs with sanitised params (email bodies truncated). Keep logging on all new actions.

## Conventions

- Tests are colocated `*_test.go`, hermetic (Google/Telegram mocked) — `go test ./...` needs no credentials or network.
- `docs/plans/` and `docs/PLAN.md` are historical build notes; `README.md` is the user-facing contract. Trust `main.go` / `router.go` / `config.go` over prose when they conflict.
