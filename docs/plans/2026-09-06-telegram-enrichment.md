# Telegram Approval Message Enrichment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Resolve opaque resource IDs into human-readable details in Telegram approval messages before the user approves or rejects.

**Architecture:** Add an optional `Enricher` interface to the service layer. Services that reference resources by ID (gcal, msft-cal, gmail, outlook) implement it to fetch and inject human-readable fields. The router calls `EnrichDetails` with up to 3 retries before sending the approval request; all 3 failures hard-block the action with `ENRICHMENT_FAILED`.

**Tech Stack:** Go 1.26.4, net/http/httptest for mocks, existing service patterns.

---

### Task 1: Add Enricher Interface

**Files:**
- Modify: `internal/service/types.go:27-29` (after `Result` struct)

**Step 1: Write the interface**

Add after the `Result` struct at the end of `internal/service/types.go`:

```go
type Enricher interface {
	EnrichDetails(action string, args map[string]string) (map[string]string, error)
}
```

`action` is the bare action name (e.g. `"delete"`, not `"gcal:delete"`) -- matches the existing `Execute(action string, ...)` convention. Returns a new map with enriched fields added, or error on lookup failure.

**Step 2: Verify it compiles**

Run: `go vet ./internal/service/`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/service/types.go
git commit -m "feat(service): add Enricher interface for approval detail resolution"
```

---

### Task 2: Integrate Enrichment into Router

**Files:**
- Modify: `internal/cli/router.go:65-158`

**Step 1: Write the failing test**

Add to `internal/cli/router_test.go`:

```go
type fakeEnricherService struct {
	fakeService
	enrichErr    error
	enrichCalls  int
	enrichResult map[string]string
}

func (f *fakeEnricherService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	f.enrichCalls++
	if f.enrichErr != nil {
		return nil, f.enrichErr
	}
	if f.enrichResult != nil {
		return f.enrichResult, nil
	}
	return args, nil
}

func TestRouterEnrichmentPassedToApprover(t *testing.T) {
	enriched := map[string]string{
		"event-id":    "evt1",
		"event-title": "Team Standup",
		"event-time":  "2024-01-15T09:00:00Z - 2024-01-15T09:30:00Z",
	}
	svc := &fakeEnricherService{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
		enrichResult: enriched,
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	var captured approval.ApprovalRequest
	approver := &capturingApprover{approved: true, capture: &captured}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Details["event-title"] != "Team Standup" {
		t.Fatalf("expected enriched details passed to approver, got: %v", captured.Details)
	}
	if svc.enrichCalls != 1 {
		t.Fatalf("expected 1 enrich call, got %d", svc.enrichCalls)
	}
}

func TestRouterEnrichmentFailureBlocks(t *testing.T) {
	svc := &fakeEnricherService{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
		enrichErr: fmt.Errorf("API unreachable"),
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err == nil {
		t.Fatal("expected error for enrichment failure")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T", err)
	}
	if re.Code != "ENRICHMENT_FAILED" {
		t.Fatalf("expected ENRICHMENT_FAILED, got %s", re.Code)
	}
	if svc.enrichCalls != 3 {
		t.Fatalf("expected 3 retry attempts, got %d", svc.enrichCalls)
	}
	if approver.called {
		t.Fatal("approver should NOT be called when enrichment fails")
	}
}

func TestRouterEnrichmentRetrySucceeds(t *testing.T) {
	svc := &fakeEnricherService{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
		enrichResult: map[string]string{
			"event-id":    "evt1",
			"event-title": "Retried Event",
		},
	}
	// Fail twice, succeed on third
	callCount := 0
	svc.enrichErr = nil // will be overridden by custom logic below

	reg := service.NewRegistry()

	failTwiceSvc := &failTwiceEnricher{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
	}
	_ = callCount
	reg.Register(failTwiceSvc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if !approver.called {
		t.Fatal("expected approver to be called after successful retry")
	}
	if failTwiceSvc.calls != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", failTwiceSvc.calls)
	}
}

type failTwiceEnricher struct {
	fakeService
	calls int
}

func (f *failTwiceEnricher) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	f.calls++
	if f.calls <= 2 {
		return nil, fmt.Errorf("temporary failure")
	}
	enriched := make(map[string]string, len(args)+1)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["event-title"] = "Retried Event"
	return enriched, nil
}
```

Also add a `capturingApprover` helper:

```go
type capturingApprover struct {
	approved bool
	capture  *approval.ApprovalRequest
}

func (c *capturingApprover) RequestApproval(_ context.Context, req approval.ApprovalRequest) (approval.ApprovalResult, error) {
	*c.capture = req
	return approval.ApprovalResult{Approved: c.approved, Message: "test"}, nil
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestRouterEnrichment" -v`
Expected: FAIL -- enrichment logic not implemented yet

**Step 3: Implement the router changes**

In `internal/cli/router.go`, modify `Dispatch`:

1. Move `svc := r.config.Registry.Get(serviceName)` from line 122 to right after the deny-tier check (after line 89).
2. Add the enrichment block between the deny-tier check and the approve-tier block:

```go
// Enrich details for approval message if the service supports it.
svc := r.config.Registry.Get(serviceName)
details := args
if tier == approval.TierApprove {
	if enricher, ok := svc.(service.Enricher); ok {
		var enrichErr error
		for attempt := 0; attempt < 3; attempt++ {
			details, enrichErr = enricher.EnrichDetails(actionName, args)
			if enrichErr == nil {
				break
			}
		}
		if enrichErr != nil {
			r.logAction(actionKey, account, args, "denied", "failure", "ENRICHMENT_FAILED")
			return nil, &RouterError{
				Code:    "ENRICHMENT_FAILED",
				Message: fmt.Sprintf("failed to resolve details for %s: %v", actionKey, enrichErr),
				Err:     enrichErr,
			}
		}
	}
}
```

3. In the approve-tier block, change `Details: args` to `Details: details`.
4. Remove the duplicate `svc := r.config.Registry.Get(serviceName)` at the old location (line 122).

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/cli/router.go internal/cli/router_test.go
git commit -m "feat(router): enrich approval details with 3-retry hard block"
```

---

### Task 3: Add ENRICHMENT_FAILED to Error Code List

**Files:**
- Modify: `internal/output/output_test.go:96-116`

**Step 1: Add the error code**

Add `"ENRICHMENT_FAILED": true,` to the `valid` map in `TestErrorCodes`.

**Step 2: Run the test**

Run: `go test ./internal/output/ -run TestErrorCodes -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/output/output_test.go
git commit -m "test(output): add ENRICHMENT_FAILED to error code list"
```

---

### Task 4: Implement gcal EnrichDetails

**Files:**
- Modify: `internal/service/gcal/service.go`
- Test: `internal/service/gcal/read_test.go`

**Step 1: Write the failing test**

Add to `internal/service/gcal/read_test.go`:

```go
func TestEnrichDetailsDelete(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "evt1",
			"summary": "Team Standup",
			"start":   map[string]string{"dateTime": "2024-01-15T09:00:00Z"},
			"end":     map[string]string{"dateTime": "2024-01-15T09:30:00Z"},
			"status":  "confirmed",
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	enriched, err := svc.EnrichDetails("delete", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["event-id"] != "evt1" {
		t.Errorf("expected event-id preserved, got %s", enriched["event-id"])
	}
	if enriched["event-title"] != "Team Standup" {
		t.Errorf("expected event-title=Team Standup, got %s", enriched["event-title"])
	}
	if enriched["event-time"] == "" {
		t.Error("expected event-time to be set")
	}
}

func TestEnrichDetailsUpdate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "evt2",
			"summary": "Weekly Review",
			"start":   map[string]string{"dateTime": "2024-01-15T14:00:00Z"},
			"end":     map[string]string{"dateTime": "2024-01-15T15:00:00Z"},
			"status":  "confirmed",
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	enriched, err := svc.EnrichDetails("update", map[string]string{
		"event-id": "evt2",
		"title":    "New Title",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["event-title"] != "Weekly Review" {
		t.Errorf("expected event-title=Weekly Review, got %s", enriched["event-title"])
	}
	if enriched["title"] != "New Title" {
		t.Errorf("expected original args preserved, got %s", enriched["title"])
	}
}

func TestEnrichDetailsNoOp(t *testing.T) {
	svc := newTestService("http://unused")
	args := map[string]string{"title": "New Event", "start": "2024-01-15T09:00:00Z", "end": "2024-01-15T10:00:00Z"}
	enriched, err := svc.EnrichDetails("create", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["title"] != "New Event" {
		t.Errorf("expected args unchanged, got %v", enriched)
	}
	if _, exists := enriched["event-title"]; exists {
		t.Error("expected no event-title for create action")
	}
}

func TestEnrichDetailsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt-missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	_, err := svc.EnrichDetails("delete", map[string]string{"event-id": "evt-missing"})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/gcal/ -run "TestEnrichDetails" -v`
Expected: FAIL -- method not implemented

**Step 3: Implement EnrichDetails**

Add to `internal/service/gcal/service.go`:

```go
func (c *CalendarService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	if action != "delete" && action != "update" {
		return args, nil
	}
	eventID := args["event-id"]
	if eventID == "" {
		return args, nil
	}
	result, err := c.getEvent(map[string]string{"event-id": eventID})
	if err != nil {
		return nil, fmt.Errorf("enriching %s: %w", action, err)
	}
	event := result.Data.(EventDetail)
	enriched := make(map[string]string, len(args)+2)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["event-title"] = event.Summary
	enriched["event-time"] = event.Start + " - " + event.End
	return enriched, nil
}
```

**Step 4: Run all gcal tests**

Run: `go test ./internal/service/gcal/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/service/gcal/service.go internal/service/gcal/read_test.go
git commit -m "feat(gcal): implement EnrichDetails for delete and update"
```

---

### Task 5: Implement msft-cal EnrichDetails

**Files:**
- Modify: `internal/service/msft-cal/service.go`
- Test: `internal/service/msft-cal/read_test.go`

**Step 1: Write the failing test**

Add to `internal/service/msft-cal/read_test.go` (follow the same mock pattern used in that file's existing tests):

```go
func TestEnrichDetailsDelete(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/events/evt1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "evt1",
			"subject": "Team Standup",
			"start":   map[string]string{"dateTime": "2024-01-15T09:00:00", "timeZone": "UTC"},
			"end":     map[string]string{"dateTime": "2024-01-15T09:30:00", "timeZone": "UTC"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	enriched, err := svc.EnrichDetails("delete", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["event-title"] != "Team Standup" {
		t.Errorf("expected event-title=Team Standup, got %s", enriched["event-title"])
	}
	if enriched["event-time"] == "" {
		t.Error("expected event-time to be set")
	}
	if enriched["event-id"] != "evt1" {
		t.Errorf("expected original args preserved")
	}
}

func TestEnrichDetailsNoOp(t *testing.T) {
	svc := newTestService("http://unused")
	args := map[string]string{"subject": "New Event", "start": "2024-01-15T09:00:00Z", "end": "2024-01-15T10:00:00Z"}
	enriched, err := svc.EnrichDetails("create", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := enriched["event-title"]; exists {
		t.Error("expected no event-title for create action")
	}
}

func TestEnrichDetailsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/events/evt-missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	_, err := svc.EnrichDetails("delete", map[string]string{"event-id": "evt-missing"})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/msft-cal/ -run "TestEnrichDetails" -v`
Expected: FAIL

**Step 3: Implement EnrichDetails**

Add to `internal/service/msft-cal/service.go`:

```go
func (s *MsftCalService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	if action != "delete" && action != "update" {
		return args, nil
	}
	eventID := args["event-id"]
	if eventID == "" {
		return args, nil
	}
	result, err := s.getEvent(map[string]string{"event-id": eventID})
	if err != nil {
		return nil, fmt.Errorf("enriching %s: %w", action, err)
	}
	event := result.Data.(EventDetail)
	enriched := make(map[string]string, len(args)+2)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["event-title"] = event.Subject
	enriched["event-time"] = event.Start + " - " + event.End
	return enriched, nil
}
```

**Step 4: Run all msft-cal tests**

Run: `go test ./internal/service/msft-cal/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/service/msft-cal/service.go internal/service/msft-cal/read_test.go
git commit -m "feat(msft-cal): implement EnrichDetails for delete and update"
```

---

### Task 6: Implement gmail EnrichDetails

**Files:**
- Modify: `internal/service/gmail/service.go`
- Test: `internal/service/gmail/read_test.go`

**Step 1: Write the failing test**

Add to `internal/service/gmail/read_test.go` (follow the existing mock pattern -- check the file for `newTestService` and `fakeTokenProvider`):

```go
func TestEnrichDetailsReply(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg1",
			"snippet": "Hey, can we meet tomorrow to discuss the project?",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "Subject", "value": "Meeting tomorrow"},
				},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	enriched, err := svc.EnrichDetails("reply", map[string]string{"id": "msg1", "body": "Sure!"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["id"] != "msg1" {
		t.Errorf("expected original args preserved")
	}
	if enriched["body"] != "Sure!" {
		t.Errorf("expected body preserved")
	}
	if enriched["original-from"] != "alice@example.com" {
		t.Errorf("expected original-from=alice@example.com, got %s", enriched["original-from"])
	}
	if enriched["original-subject"] != "Meeting tomorrow" {
		t.Errorf("expected original-subject=Meeting tomorrow, got %s", enriched["original-subject"])
	}
	if enriched["original-snippet"] == "" {
		t.Error("expected original-snippet to be set")
	}
}

func TestEnrichDetailsNoOpSend(t *testing.T) {
	svc := newTestService("http://unused")
	args := map[string]string{"to": "bob@example.com", "subject": "Hi", "body": "Hello"}
	enriched, err := svc.EnrichDetails("send", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := enriched["original-from"]; exists {
		t.Error("expected no enrichment for send action")
	}
}

func TestEnrichDetailsReplyAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	_, err := svc.EnrichDetails("reply", map[string]string{"id": "msg-gone", "body": "Reply"})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/gmail/ -run "TestEnrichDetails" -v`
Expected: FAIL

**Step 3: Implement EnrichDetails**

Add to `internal/service/gmail/service.go`:

```go
func (g *GmailService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	if action != "reply" {
		return args, nil
	}
	msgID := args["id"]
	if msgID == "" {
		return args, nil
	}
	msg, err := g.fetchFullMessage(msgID)
	if err != nil {
		return nil, fmt.Errorf("enriching reply: %w", err)
	}
	enriched := make(map[string]string, len(args)+3)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["original-from"] = msg.From
	enriched["original-subject"] = msg.Subject
	snippet := msg.Snippet
	if len(snippet) > 100 {
		snippet = snippet[:100] + "..."
	}
	enriched["original-snippet"] = snippet
	return enriched, nil
}
```

**Step 4: Run all gmail tests**

Run: `go test ./internal/service/gmail/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/service/gmail/service.go internal/service/gmail/read_test.go
git commit -m "feat(gmail): implement EnrichDetails for reply"
```

---

### Task 7: Implement outlook EnrichDetails

**Files:**
- Modify: `internal/service/outlook/service.go`
- Test: `internal/service/outlook/read_test.go`

**Step 1: Write the failing test**

Add to `internal/service/outlook/read_test.go` (follow the existing mock pattern):

```go
func TestEnrichDetailsReply(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":               "msg1",
			"subject":          "Project Update",
			"bodyPreview":      "Here is the latest update on the project status for this quarter.",
			"receivedDateTime": "2024-01-15T10:00:00Z",
			"from": map[string]any{
				"emailAddress": map[string]string{"address": "alice@example.com"},
			},
			"toRecipients": []map[string]any{
				{"emailAddress": map[string]string{"address": "bob@example.com"}},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	enriched, err := svc.EnrichDetails("reply", map[string]string{"id": "msg1", "body": "Thanks!"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched["original-from"] != "alice@example.com" {
		t.Errorf("expected original-from=alice@example.com, got %s", enriched["original-from"])
	}
	if enriched["original-subject"] != "Project Update" {
		t.Errorf("expected original-subject=Project Update, got %s", enriched["original-subject"])
	}
	if enriched["original-snippet"] == "" {
		t.Error("expected original-snippet to be set")
	}
	if enriched["id"] != "msg1" {
		t.Errorf("expected original args preserved")
	}
}

func TestEnrichDetailsNoOpSend(t *testing.T) {
	svc := newTestService("http://unused")
	args := map[string]string{"to": "bob@example.com", "subject": "Hi", "body": "Hello"}
	enriched, err := svc.EnrichDetails("send", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := enriched["original-from"]; exists {
		t.Error("expected no enrichment for send action")
	}
}

func TestEnrichDetailsReplyAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages/msg-gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	_, err := svc.EnrichDetails("reply", map[string]string{"id": "msg-gone", "body": "Reply"})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/outlook/ -run "TestEnrichDetails" -v`
Expected: FAIL

**Step 3: Implement EnrichDetails**

The outlook `get` method fetches a single message and returns a `MessageDetail`. For enrichment, call `o.get(...)` internally.

Add to `internal/service/outlook/service.go`:

```go
func (o *OutlookService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	if action != "reply" {
		return args, nil
	}
	msgID := args["id"]
	if msgID == "" {
		return args, nil
	}
	result, err := o.get(map[string]string{"id": msgID})
	if err != nil {
		return nil, fmt.Errorf("enriching reply: %w", err)
	}
	msg := result.Data.(MessageDetail)
	enriched := make(map[string]string, len(args)+3)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["original-from"] = msg.From
	enriched["original-subject"] = msg.Subject
	snippet := msg.Snippet
	if len(snippet) > 100 {
		snippet = snippet[:100] + "..."
	}
	enriched["original-snippet"] = snippet
	return enriched, nil
}
```

**Step 4: Run all outlook tests**

Run: `go test ./internal/service/outlook/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/service/outlook/service.go internal/service/outlook/read_test.go
git commit -m "feat(outlook): implement EnrichDetails for reply"
```

---

### Task 8: Full Integration Test

**Files:** None (test-only)

**Step 1: Run the full test suite**

Run: `go test ./... -v`
Expected: all PASS, no regressions

**Step 2: Run go vet**

Run: `go vet ./...`
Expected: no issues

**Step 3: Verify build**

Run: `go build -o kuma-approve ./cmd/kuma-approve/`
Expected: clean build, binary produced
