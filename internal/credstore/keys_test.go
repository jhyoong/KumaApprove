package credstore

import "testing"

func TestDeriveKey(t *testing.T) {
	key1, err := DeriveKey("test-machine-id-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key1))
	}

	key2, err := DeriveKey("test-machine-id-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) != string(key2) {
		t.Fatal("same input must produce same key")
	}

	key3, err := DeriveKey("different-machine-id-999")
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) == string(key3) {
		t.Fatal("different inputs must produce different keys")
	}
}

func TestDeriveKeyRejectsEmpty(t *testing.T) {
	_, err := DeriveKey("")
	if err == nil {
		t.Fatal("expected error for empty machine ID")
	}
}
