# Telegram Approval Message Enrichment

## Problem

When the Telegram bot sends approval requests for actions that reference resources by opaque ID (e.g. `gcal:delete` with only an `event-id`), the approver cannot tell what resource is being acted on. They see a meaningless ID string and must approve or reject blindly.

## Approach: Enricher Interface

Add an optional `Enricher` interface to the service layer. Services that implement it resolve opaque IDs into human-readable details before the approval message is sent.

## Enricher Interface

New optional interface in `internal/service/types.go`:

```go
type Enricher interface {
    EnrichDetails(action string, args map[string]string) (map[string]string, error)
}
```

Returns a new map with human-readable fields added alongside the original args. Returns an error if the lookup fails.

## Router Integration

In `internal/cli/router.go`, before `RequestApproval`:

1. Move `Registry.Get(serviceName)` above the approval gate.
2. Type-assert the service as `service.Enricher`.
3. Call `EnrichDetails` up to 3 times (retry on failure).
4. If all 3 attempts fail, return `RouterError{Code: "ENRICHMENT_FAILED"}` -- hard block, no approval message sent.
5. Pass enriched `details` to `ApprovalRequest.Details`. Pass original `args` to `Execute()`.

## Service Implementations

### gcal (Google Calendar)

- `gcal:delete`, `gcal:update`: Fetch event by `event-id`, add `event-title` and `event-time` (formatted "start - end").
- All other actions: Return `args` unchanged.

### msft-cal (Microsoft Calendar)

- `msft-cal:delete`, `msft-cal:update`: Same as gcal.

### gmail

- `gmail:reply`: Fetch message by `id`, add `original-subject`, `original-from`, `original-snippet` (first ~100 chars).

### outlook

- `outlook:reply`: Same as gmail.

### exec

- Does not implement `Enricher` -- `cmd` arg is already human-readable.

## Example Output

Before:
```
Details:
  event-id: 81n0he347lam7g3em6b3q4g174
```

After:
```
Details:
  event-id: 81n0he347lam7g3em6b3q4g174
  event-title: Team standup
  event-time: 2026-09-06 09:00 - 2026-09-06 09:30
```

## Error Handling

- Enrichment is retried up to 3 times.
- If all attempts fail, the action is blocked with `ENRICHMENT_FAILED` error code.
- Rationale: approving a blind ID defeats the purpose of human approval.

## Testing

- Happy path: mock returns resource JSON, verify enriched fields present.
- Retry success: mock fails twice then succeeds, verify enrichment works.
- Hard failure: mock fails 3 times, verify `ENRICHMENT_FAILED` returned, no approval sent.
- No-op actions: verify `args` returned unchanged for actions that don't need enrichment.
- Router tests: verify enriched details passed to approver, verify hard block on failure.
- Add `ENRICHMENT_FAILED` to output error code list.
