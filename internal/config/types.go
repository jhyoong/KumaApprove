package config

// Config is the top-level configuration for KumaApprove,
// stored at ~/.kuma-approve/config.json.
type Config struct {
	Telegram    TelegramConfig      `json:"telegram"`
	Approval    ApprovalConfig      `json:"approval"`
	Exec        ExecConfig          `json:"exec"`
	GoogleOAuth GoogleOAuthConfig   `json:"google_oauth"`
	Accounts    map[string][]string `json:"accounts"`
}

// TelegramConfig holds Telegram bot credentials.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// ApprovalConfig controls approval behavior and tier mappings.
type ApprovalConfig struct {
	TimeoutMinutes int               `json:"timeout_minutes"`
	Tiers          map[string]string `json:"tiers"`
}

// ExecConfig controls command execution settings.
type ExecConfig struct {
	WorkingDirectory string   `json:"working_directory"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	MaxOutputBytes   int      `json:"max_output_bytes"`
	SafeList         []string `json:"safe_list"`
	DenyListPatterns []string `json:"deny_list_patterns"`
}

// GoogleOAuthConfig holds Google OAuth client credentials.
type GoogleOAuthConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}
