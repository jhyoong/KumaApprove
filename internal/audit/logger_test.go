package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	err = logger.Log(Entry{
		Action:  "calendar.create",
		Account: "user@example.com",
		Status:  "approved",
		Result:  "success",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if entry.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if entry.Action == "" {
		t.Error("expected non-empty action")
	}
}

func TestMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	for i := 0; i < 3; i++ {
		err = logger.Log(Entry{
			Action:  "test.action",
			Account: "user@example.com",
			Status:  "approved",
			Result:  "success",
		})
		if err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: Unmarshal: %v", i, err)
		}
	}
}

func TestLogSanitisesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	longBody := strings.Repeat("x", 1000)
	err = logger.Log(Entry{
		Action:  "email.send",
		Account: "user@example.com",
		Params:  map[string]string{"body": longBody, "subject": "hello"},
		Status:  "auto",
		Result:  "success",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	body := entry.Params["body"]
	if len(body) > 203 {
		t.Errorf("expected body <= 203 chars, got %d", len(body))
	}
	if !strings.HasSuffix(body, "...") {
		t.Error("expected truncated body to end with '...'")
	}

	// subject should be unchanged
	if entry.Params["subject"] != "hello" {
		t.Errorf("expected subject 'hello', got %q", entry.Params["subject"])
	}
}

func TestLogCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "audit.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	err = logger.Log(Entry{
		Action:  "test.create",
		Account: "user@example.com",
		Status:  "approved",
		Result:  "success",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}
