package output

import (
	"encoding/json"
	"testing"
)

func TestSuccessEnvelope(t *testing.T) {
	data := map[string]string{"id": "abc123"}
	env := Success("gmail:list", data)

	if !env.Success {
		t.Fatal("expected success=true")
	}
	if env.Action != "gmail:list" {
		t.Fatalf("expected action gmail:list, got %s", env.Action)
	}
	if env.Error != nil {
		t.Fatal("expected error=nil")
	}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	if raw["error"] != nil {
		t.Fatal("expected null error in JSON")
	}
}

func TestErrorEnvelope(t *testing.T) {
	env := Fail("gmail:send", "APPROVAL_REJECTED", "Action was rejected by user via Telegram")

	if env.Success {
		t.Fatal("expected success=false")
	}
	if env.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if env.Error.Code != "APPROVAL_REJECTED" {
		t.Fatalf("expected APPROVAL_REJECTED, got %s", env.Error.Code)
	}
	if env.Data != nil {
		t.Fatal("expected data=nil")
	}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	if raw["data"] != nil {
		t.Fatal("expected null data in JSON")
	}
}

func TestErrorCodes(t *testing.T) {
	valid := map[string]bool{
		"APPROVAL_REJECTED": true,
		"APPROVAL_TIMEOUT":  true,
		"AUTH_EXPIRED":      true,
		"DENIED_BY_POLICY":  true,
		"EXECUTION_TIMEOUT": true,
		"API_ERROR":         true,
		"INVALID_ARGS":      true,
	}
	for code := range valid {
		env := Fail("test", code, "msg")
		if env.Error.Code != code {
			t.Fatalf("code mismatch: %s", code)
		}
	}
}
