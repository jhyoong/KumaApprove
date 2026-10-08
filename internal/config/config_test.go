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
	if IsRoot() {
		t.Skip("root uses the fixed RootDir")
	}
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

// privateDir returns a temp dir only its owner can write, whatever the umask.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerifyOwnedBy(t *testing.T) {
	uid := uint32(os.Getuid())

	t.Run("private file in private dir", func(t *testing.T) {
		dir := privateDir(t)
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyOwnedBy(path, uid); err != nil {
			t.Fatalf("expected config to be accepted, got %v", err)
		}
	})

	t.Run("wrong owner", func(t *testing.T) {
		dir := privateDir(t)
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyOwnedBy(path, uid+1); err == nil {
			t.Fatal("expected a config owned by another user to be rejected")
		}
	})

	t.Run("file readable by others", func(t *testing.T) {
		dir := privateDir(t)
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyOwnedBy(path, uid); err == nil {
			t.Fatal("expected a world-readable config to be rejected")
		}
	})

	t.Run("directory writable by others", func(t *testing.T) {
		dir := privateDir(t)
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0777); err != nil {
			t.Fatal(err)
		}
		if err := verifyOwnedBy(path, uid); err == nil {
			t.Fatal("expected a config in a world-writable directory to be rejected")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "real.json")
		if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "config.json")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := verifyOwnedBy(path, uid); err == nil {
			t.Fatal("expected a symlinked config to be rejected")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if err := verifyOwnedBy(filepath.Join(privateDir(t), "config.json"), uid); err == nil {
			t.Fatal("expected a missing config to be rejected")
		}
	})
}

func TestDirIgnoresHomeAsRoot(t *testing.T) {
	if !IsRoot() {
		t.Skip("requires root")
	}
	t.Setenv("HOME", "/tmp/testhome")
	if dir := Dir(); dir != RootDir {
		t.Fatalf("expected %s as root regardless of HOME, got %s", RootDir, dir)
	}
	if wd := Default().Exec.WorkingDirectory; wd != "/root" {
		t.Fatalf("expected root working directory /root, got %s", wd)
	}
}

func TestTraceRoundTrip(t *testing.T) {
	if Default().Approval.Trace {
		t.Fatal("expected trace to be off by default")
	}

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Approval.Trace = true
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Approval.Trace {
		t.Fatal("expected trace setting to survive save and load")
	}
}
