package credstore

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}

	cred := Credential{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		Expiry:       "2026-09-03T15:00:00Z",
	}
	err = store.Put("gmail:personal@gmail.com", cred)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("gmail:personal@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access-123" {
		t.Fatalf("expected access-123, got %s", got.AccessToken)
	}
	if got.RefreshToken != "refresh-456" {
		t.Fatalf("expected refresh-456, got %s", got.RefreshToken)
	}
}

func TestStoreGetMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)

	store, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Get("nonexistent:key")
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
}

func TestStoreDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)

	store, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}

	store.Put("gmail:test@gmail.com", Credential{AccessToken: "a"})
	err = store.Delete("gmail:test@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Get("gmail:test@gmail.com")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store1, _ := NewStore(path, key)
	store1.Put("gmail:a@b.com", Credential{AccessToken: "token-abc"})

	store2, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store2.Get("gmail:a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "token-abc" {
		t.Fatalf("expected token-abc, got %s", got.AccessToken)
	}
}

func TestStoreWrongKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 0xFF

	store1, _ := NewStore(path, key1)
	store1.Put("gmail:a@b.com", Credential{AccessToken: "secret"})

	_, err := NewStore(path, key2)
	if err == nil {
		t.Fatal("expected error when loading with wrong key")
	}
}

func TestStoreList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)

	store, _ := NewStore(path, key)
	store.Put("gmail:a@b.com", Credential{AccessToken: "1"})
	store.Put("gcal:a@b.com", Credential{AccessToken: "2"})

	keys := store.List()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}
