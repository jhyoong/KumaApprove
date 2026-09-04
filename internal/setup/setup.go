package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/auth"
	"github.com/jhyoong/KumaApprove/internal/config"
	"github.com/jhyoong/KumaApprove/internal/credstore"
)

type sectionStatus struct {
	configured bool
	valid      bool
	reason     string
	detail     string
}

func validateTelegramBot(botToken, apiBase string) error {
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	url := fmt.Sprintf("%s/bot%s/getMe", apiBase, botToken)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("connecting to Telegram API: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parsing Telegram response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("bot token invalid: %s", result.Description)
	}
	return nil
}

// readLine prints the prompt to stdout, reads one line from reader,
// trims whitespace, and returns the result.
func readLine(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(os.Stderr, "\nsetup cancelled")
		os.Exit(1)
	}
	return strings.TrimSpace(line)
}

// Run executes the interactive setup wizard.
func Run() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=== KumaApprove Setup Wizard ===")
	fmt.Println()

	// Create config directory and workspace.
	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create config directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(config.Dir(), "workspace"), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create workspace directory: %v\n", err)
		os.Exit(1)
	}

	// Start with default config.
	cfg := config.Default()

	// Telegram configuration.
	botToken := readLine(reader, "Telegram bot token: ")
	cfg.Telegram.BotToken = botToken

	chatID, err := detectChatID(botToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to detect chat ID: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Detected chat ID: %s\n", chatID)
	cfg.Telegram.ChatID = chatID

	// Google OAuth configuration.
	clientID := readLine(reader, "Google OAuth client ID: ")
	clientSecret := readLine(reader, "Google OAuth client secret: ")
	cfg.GoogleOAuth.ClientID = clientID
	cfg.GoogleOAuth.ClientSecret = clientSecret

	// Google account.
	account := readLine(reader, "Google account email: ")
	cfg.Accounts = map[string][]string{
		"gmail": {account},
		"gcal":  {account},
	}

	// Save config.
	if err := config.Save(cfg, config.DefaultPath()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Config saved.")

	// Set up credential store.
	machineID, err := credstore.GetMachineID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get machine ID: %v\n", err)
		os.Exit(1)
	}

	encKey, err := credstore.DeriveKey(machineID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to derive key: %v\n", err)
		os.Exit(1)
	}

	storePath := filepath.Join(config.Dir(), "credentials.enc")
	store, err := credstore.NewStore(storePath, encKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open credential store: %v\n", err)
		os.Exit(1)
	}

	// Run OAuth flows.
	gauth := &auth.GoogleAuth{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Store:        store,
	}

	fmt.Println()
	fmt.Println("Authorizing Gmail...")
	if err := gauth.RunOAuthFlow("gmail", account); err != nil {
		fmt.Fprintf(os.Stderr, "Gmail OAuth failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Authorizing Google Calendar...")
	if err := gauth.RunOAuthFlow("gcal", account); err != nil {
		fmt.Fprintf(os.Stderr, "Google Calendar OAuth failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	msSetup := readLine(reader, "Configure Microsoft Outlook/Calendar? (y/n): ")
	if strings.ToLower(msSetup) == "y" {
		msClientID := readLine(reader, "Microsoft Azure AD client ID: ")
		msClientSecret := readLine(reader, "Microsoft Azure AD client secret: ")
		msTenantID := readLine(reader, "Microsoft tenant ID (press Enter for 'consumers'): ")
		if msTenantID == "" {
			msTenantID = "consumers"
		}

		cfg.MicrosoftOAuth = config.MicrosoftOAuthConfig{
			ClientID:     msClientID,
			ClientSecret: msClientSecret,
			TenantID:     msTenantID,
		}

		msAccount := readLine(reader, "Microsoft account email: ")

		cfg.Accounts["outlook"] = []string{msAccount}
		cfg.Accounts["msft-cal"] = []string{msAccount}

		if err := config.Save(cfg, config.DefaultPath()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
			os.Exit(1)
		}

		msauth := &auth.MicrosoftAuth{
			ClientID:     msClientID,
			ClientSecret: msClientSecret,
			TenantID:     msTenantID,
			Store:        store,
		}

		fmt.Println()
		fmt.Println("Authorizing Outlook...")
		if err := msauth.RunOAuthFlow("outlook", msAccount); err != nil {
			fmt.Fprintf(os.Stderr, "Outlook OAuth failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Authorizing Microsoft Calendar...")
		if err := msauth.RunOAuthFlow("msft-cal", msAccount); err != nil {
			fmt.Fprintf(os.Stderr, "Microsoft Calendar OAuth failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Microsoft services configured.")
	}

	// Send test Telegram message.
	if err := sendTestMessage(botToken, chatID); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send test message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Setup complete! KumaApprove is ready to use.")
}

// RunAuth re-authorizes a single service for a given account.
func RunAuth(serviceName, account string) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	machineID, err := credstore.GetMachineID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get machine ID: %v\n", err)
		os.Exit(1)
	}

	encKey, err := credstore.DeriveKey(machineID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to derive key: %v\n", err)
		os.Exit(1)
	}

	storePath := filepath.Join(config.Dir(), "credentials.enc")
	store, err := credstore.NewStore(storePath, encKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open credential store: %v\n", err)
		os.Exit(1)
	}

	switch serviceName {
	case "outlook", "msft-cal":
		msauth := &auth.MicrosoftAuth{
			ClientID:     cfg.MicrosoftOAuth.ClientID,
			ClientSecret: cfg.MicrosoftOAuth.ClientSecret,
			TenantID:     cfg.MicrosoftOAuth.TenantID,
			Store:        store,
		}
		fmt.Printf("Authorizing %s for %s...\n", serviceName, account)
		if err := msauth.RunOAuthFlow(serviceName, account); err != nil {
			fmt.Fprintf(os.Stderr, "OAuth failed: %v\n", err)
			os.Exit(1)
		}
	default:
		gauth := &auth.GoogleAuth{
			ClientID:     cfg.GoogleOAuth.ClientID,
			ClientSecret: cfg.GoogleOAuth.ClientSecret,
			Store:        store,
		}
		fmt.Printf("Authorizing %s for %s...\n", serviceName, account)
		if err := gauth.RunOAuthFlow(serviceName, account); err != nil {
			fmt.Fprintf(os.Stderr, "OAuth failed: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Authorization for %s:%s complete.\n", serviceName, account)
}

// detectChatID calls the Telegram getUpdates API and extracts the chat ID
// from the first message result.
func detectChatID(botToken string) (string, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", botToken)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("calling getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Telegram API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message struct {
				Chat struct {
					ID json.Number `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK || len(result.Result) == 0 {
		return "", fmt.Errorf("no updates found; send a message to your bot first, then re-run setup")
	}

	chatID := result.Result[0].Message.Chat.ID.String()
	if chatID == "" {
		return "", fmt.Errorf("no chat ID found in updates; send a message to your bot first")
	}

	return chatID, nil
}

// sendTestMessage sends a confirmation message to the Telegram chat.
func sendTestMessage(botToken, chatID string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	resp, err := http.PostForm(url, map[string][]string{
		"chat_id": {chatID},
		"text":    {"KumaApprove setup complete! This bot is ready to handle approvals."},
	})
	if err != nil {
		return fmt.Errorf("sending test message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
