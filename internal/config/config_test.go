package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()

	if cfg.Approval.TimeoutMinutes != 240 {
		t.Fatalf("expected default timeout 240, got %d", cfg.Approval.TimeoutMinutes)
	}
	if cfg.Exec.TimeoutSeconds != 60 {
		t.Fatalf("expected exec timeout 60, got %d", cfg.Exec.TimeoutSeconds)
	}
	if cfg.Exec.MaxOutputBytes != 102400 {
		t.Fatalf("expected max output 102400, got %d", cfg.Exec.MaxOutputBytes)
	}
	if len(cfg.Approval.Tiers) == 0 {
		t.Fatal("expected default tiers to be populated")
	}
	if cfg.Approval.Tiers["gmail:list"] != "auto" {
		t.Fatalf("expected gmail:list=auto, got %s", cfg.Approval.Tiers["gmail:list"])
	}
	if cfg.Approval.Tiers["gmail:send"] != "approve" {
		t.Fatalf("expected gmail:send=approve, got %s", cfg.Approval.Tiers["gmail:send"])
	}

	path := filepath.Join(dir, "config.json")
	err := Save(cfg, path)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Approval.TimeoutMinutes != 240 {
		t.Fatal("loaded config timeout mismatch")
	}
	if loaded.Approval.Tiers["exec:run"] != "approve" {
		t.Fatal("loaded config tier mismatch")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestConfigDir(t *testing.T) {
	t.Setenv("HOME", "/tmp/testhome")
	dir := Dir()
	if dir != "/tmp/testhome/.kuma-approve" {
		t.Fatalf("expected /tmp/testhome/.kuma-approve, got %s", dir)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "config.json")
	err := Save(Default(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("config file not created")
	}
}
