package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// RootDir is the config directory used when running as root. It is a fixed
// path (never derived from $HOME) so an unprivileged caller cannot point a
// root process at a config file it controls.
const RootDir = "/etc/kuma-approve"

// IsRoot reports whether the process is running with root privileges.
func IsRoot() bool {
	return os.Geteuid() == 0
}

// Dir returns the KumaApprove config directory path: ~/.kuma-approve, or
// RootDir when running as root.
func Dir() string {
	if IsRoot() {
		return RootDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kuma-approve")
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	return filepath.Join(Dir(), "config.json")
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	workDir := filepath.Join(Dir(), "workspace")
	if IsRoot() {
		workDir = "/root"
	}
	return Config{
		Approval: ApprovalConfig{
			TimeoutMinutes: 240,
			Tiers: map[string]string{
				"gmail:list":   "auto",
				"gmail:get":    "auto",
				"gmail:search": "auto",
				"gmail:send":   "approve",
				"gmail:reply":  "approve",
				"gmail:draft":  "auto",
				"gcal:list":    "auto",
				"gcal:get":     "auto",
				"gcal:create":  "approve",
				"gcal:update":  "approve",
				"gcal:delete":  "approve",
				"exec:run":     "approve",
				"outlook:list":    "auto",
				"outlook:get":     "auto",
				"outlook:search":  "auto",
				"outlook:send":    "approve",
				"outlook:reply":   "approve",
				"outlook:draft":   "auto",
				"msft-cal:list":   "auto",
				"msft-cal:get":    "auto",
				"msft-cal:create": "approve",
				"msft-cal:update": "approve",
				"msft-cal:delete": "approve",
			},
		},
		Exec: ExecConfig{
			WorkingDirectory: workDir,
			TimeoutSeconds:   60,
			MaxOutputBytes:   102400,
			SafeList: []string{
				"ls", "cat", "df", "date", "echo", "pwd",
				"wc", "head", "tail", "grep", "find",
			},
			DenyListPatterns: []string{
				`^sudo\b`,
				`^rm\s+-rf`,
				`^shutdown`,
				`^reboot`,
				`^mkfs`,
				`^dd\b`,
			},
		},
		Accounts: map[string][]string{},
	}
}

// Load reads and parses a config file from the given path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// VerifyRootOwned checks that the config file at path, and the directory
// holding it, can only be modified by root. A root process must not act on a
// config that an unprivileged user could have written.
func VerifyRootOwned(path string) error {
	return verifyOwnedBy(path, 0)
}

func verifyOwnedBy(path string, uid uint32) error {
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !dir.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Dir(path))
	}
	if err := checkOwner(dir, uid); err != nil {
		return err
	}
	if dir.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("%s is writable by group or others (mode %04o)", filepath.Dir(path), dir.Mode().Perm())
	}

	file, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !file.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err := checkOwner(file, uid); err != nil {
		return err
	}
	if file.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%s is accessible by group or others (mode %04o)", path, file.Mode().Perm())
	}
	return nil
}

func checkOwner(info os.FileInfo, uid uint32) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine owner of %s", info.Name())
	}
	if st.Uid != uid {
		return fmt.Errorf("%s is owned by uid %d, expected uid %d", info.Name(), st.Uid, uid)
	}
	return nil
}

// Save writes the config to the given path as indented JSON.
// Parent directories are created if they do not exist.
func Save(cfg Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
