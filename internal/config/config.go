package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Dir returns the KumaApprove config directory path (~/.kuma-approve).
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kuma-approve")
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	return filepath.Join(Dir(), "config.json")
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
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
			},
		},
		Exec: ExecConfig{
			WorkingDirectory: filepath.Join(Dir(), "workspace"),
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
