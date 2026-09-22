package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCallbackStoresCode(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest("GET", "/callback?state=session-1&code=auth-code-123", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html content type, got %s", ct)
	}

	h.mu.Lock()
	entry, ok := h.sessions["session-1"]
	h.mu.Unlock()
	if !ok {
		t.Fatal("session not stored")
	}
	if entry.code != "auth-code-123" {
		t.Fatalf("expected auth-code-123, got %s", entry.code)
	}
}

func TestCallbackMissingState(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest("GET", "/callback?code=auth-code-123", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPollReturnsCodeAndDeletes(t *testing.T) {
	h := NewHandler()

	h.mu.Lock()
	h.sessions["session-1"] = sessionEntry{code: "auth-code-123", created: time.Now()}
	h.mu.Unlock()

	req := httptest.NewRequest("GET", "/poll?session=session-1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "complete" {
		t.Fatalf("expected complete, got %s", result["status"])
	}
	if result["code"] != "auth-code-123" {
		t.Fatalf("expected auth-code-123, got %s", result["code"])
	}

	h.mu.Lock()
	_, ok := h.sessions["session-1"]
	h.mu.Unlock()
	if ok {
		t.Fatal("session should have been deleted after retrieval")
	}
}

func TestPollPending(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest("GET", "/poll?session=unknown-session", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "pending" {
		t.Fatalf("expected pending, got %s", result["status"])
	}
}

func TestPollMissingSession(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest("GET", "/poll", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSweepRemovesExpiredSessions(t *testing.T) {
	h := NewHandler()

	h.mu.Lock()
	h.sessions["old"] = sessionEntry{code: "old-code", created: time.Now().Add(-15 * time.Minute)}
	h.sessions["new"] = sessionEntry{code: "new-code", created: time.Now()}
	h.mu.Unlock()

	h.sweep(10 * time.Minute)

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.sessions["old"]; ok {
		t.Fatal("expired session should have been swept")
	}
	if _, ok := h.sessions["new"]; !ok {
		t.Fatal("fresh session should not have been swept")
	}
}

func TestCallbackThenPollEndToEnd(t *testing.T) {
	h := NewHandler()

	cbReq := httptest.NewRequest("GET", "/callback?state=e2e-session&code=e2e-code", nil)
	cbRR := httptest.NewRecorder()
	h.ServeHTTP(cbRR, cbReq)

	if cbRR.Code != http.StatusOK {
		t.Fatalf("callback: expected 200, got %d", cbRR.Code)
	}

	pollReq := httptest.NewRequest("GET", "/poll?session=e2e-session", nil)
	pollRR := httptest.NewRecorder()
	h.ServeHTTP(pollRR, pollReq)

	var result map[string]string
	if err := json.NewDecoder(pollRR.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "complete" || result["code"] != "e2e-code" {
		t.Fatalf("expected complete/e2e-code, got %s/%s", result["status"], result["code"])
	}

	pollReq2 := httptest.NewRequest("GET", "/poll?session=e2e-session", nil)
	pollRR2 := httptest.NewRecorder()
	h.ServeHTTP(pollRR2, pollReq2)

	var result2 map[string]string
	json.NewDecoder(pollRR2.Body).Decode(&result2)
	if result2["status"] != "pending" {
		t.Fatal("second poll should return pending (one-time read)")
	}
}

func TestNotFoundForUnknownPath(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest("GET", "/unknown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
