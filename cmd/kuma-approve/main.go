package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/audit"
	"github.com/jhyoong/KumaApprove/internal/auth"
	"github.com/jhyoong/KumaApprove/internal/cli"
	"github.com/jhyoong/KumaApprove/internal/config"
	"github.com/jhyoong/KumaApprove/internal/credstore"
	"github.com/jhyoong/KumaApprove/internal/executor"
	"github.com/jhyoong/KumaApprove/internal/output"
	"github.com/jhyoong/KumaApprove/internal/service"
	"github.com/jhyoong/KumaApprove/internal/service/gcal"
	"github.com/jhyoong/KumaApprove/internal/service/gmail"
	msftcal "github.com/jhyoong/KumaApprove/internal/service/msft-cal"
	"github.com/jhyoong/KumaApprove/internal/service/outlook"
	"github.com/jhyoong/KumaApprove/internal/setup"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	if command == "help" || command == "--help" || command == "-h" {
		printUsage()
		return
	}

	if command == "setup" {
		runSetup()
		return
	}

	if command == "auth" {
		runAuth(os.Args[2:])
		return
	}

	serviceName := command

	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: kuma-approve %s <action> [flags]\n", serviceName)
		os.Exit(1)
	}

	actionName := os.Args[2]
	args := parseFlags(os.Args[3:])

	// Load config.
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"CONFIG_ERROR",
			fmt.Sprintf("failed to load config: %v", err),
		))
		return
	}

	// Get machine ID and derive encryption key.
	machineID, err := credstore.GetMachineID()
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"MACHINE_ID_ERROR",
			fmt.Sprintf("failed to get machine ID: %v", err),
		))
		return
	}

	encKey, err := credstore.DeriveKey(machineID)
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"KEY_ERROR",
			fmt.Sprintf("failed to derive key: %v", err),
		))
		return
	}

	// Open credential store.
	storePath := filepath.Join(config.Dir(), "credentials.enc")
	store, err := credstore.NewStore(storePath, encKey)
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"CREDSTORE_ERROR",
			fmt.Sprintf("failed to open credential store: %v", err),
		))
		return
	}

	// Create Google auth provider.
	gauth := &auth.GoogleAuth{
		ClientID:     cfg.GoogleOAuth.ClientID,
		ClientSecret: cfg.GoogleOAuth.ClientSecret,
		Store:        store,
	}

	// Create Microsoft auth provider (if configured).
	var msauth *auth.MicrosoftAuth
	if cfg.MicrosoftOAuth.ClientID != "" {
		msauth = &auth.MicrosoftAuth{
			ClientID: cfg.MicrosoftOAuth.ClientID,
			TenantID: cfg.MicrosoftOAuth.TenantID,
			Store:    store,
		}
	}

	// Resolve account.
	account := resolveAccount(args, cfg, serviceName, actionName)

	// Create service registry.
	registry := service.NewRegistry()
	registry.Register(gmail.New(gauth, account))
	registry.Register(gcal.New(gauth, account))
	if msauth != nil {
		registry.Register(outlook.New(msauth, account))
		registry.Register(msftcal.New(msauth, account))
	}

	// Register executor service.
	execSafeList := make(map[string]string, len(cfg.Exec.SafeList))
	for _, cmd := range cfg.Exec.SafeList {
		execSafeList[cmd] = approval.TierAuto
	}
	execSvc, err := executor.New(executor.ExecConfig{
		WorkingDirectory: cfg.Exec.WorkingDirectory,
		TimeoutSeconds:   cfg.Exec.TimeoutSeconds,
		MaxOutputBytes:   cfg.Exec.MaxOutputBytes,
		SafeList:         execSafeList,
		DenyListPatterns: cfg.Exec.DenyListPatterns,
	})
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"EXEC_INIT_ERROR",
			fmt.Sprintf("failed to initialize executor: %v", err),
		))
		return
	}
	registry.Register(execSvc)

	// Create Telegram approver if configured.
	var approver approval.Approver
	var notifier approval.Notifier
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		tg := approval.NewTelegramApprover(approval.TelegramConfig{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
		})
		approver = tg
		notifier = tg
	}

	// Create audit logger.
	auditPath := filepath.Join(config.Dir(), "audit.log")
	logger, err := audit.NewLogger(auditPath)
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"AUDIT_ERROR",
			fmt.Sprintf("failed to create audit logger: %v", err),
		))
		return
	}

	// Create router and dispatch.
	router := cli.NewRouter(cli.RouterConfig{
		Registry:       registry,
		Approver:       approver,
		Notifier:       notifier,
		TierOverrides:  cfg.Approval.Tiers,
		Logger:         logger,
		TimeoutMinutes: cfg.Approval.TimeoutMinutes,
	})

	result, err := router.Dispatch(serviceName, actionName, account, args)
	if err != nil {
		output.PrintAndExit(output.Fail(
			serviceName+":"+actionName,
			"DISPATCH_ERROR",
			err.Error(),
		))
		return
	}

	output.PrintAndExit(output.Success(serviceName+":"+actionName, result))
}

// resolveAccount determines the account to use. It checks the --account flag
// first, then falls back to the single configured account for the service.
// Returns empty string for services with no accounts (e.g. exec).
func resolveAccount(args map[string]string, cfg config.Config, serviceName, actionName string) string {
	if acct, ok := args["account"]; ok {
		delete(args, "account")
		return acct
	}

	accounts := cfg.Accounts[serviceName]
	if len(accounts) == 0 {
		return ""
	}
	if len(accounts) == 1 {
		return accounts[0]
	}

	output.PrintAndExit(output.Fail(
		serviceName+":"+actionName,
		"NO_ACCOUNT",
		fmt.Sprintf("multiple accounts for %s: %v; use --account to specify", serviceName, accounts),
	))
	return "" // unreachable
}

func runSetup() {
	setup.Run()
}

func runAuth(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kuma-approve auth <service> <account>")
		os.Exit(1)
	}
	serviceName := args[0]
	account := args[1]

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
			ClientID: cfg.MicrosoftOAuth.ClientID,
			TenantID: cfg.MicrosoftOAuth.TenantID,
			Store:    store,
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

func parseFlags(raw []string) map[string]string {
	args := make(map[string]string)
	for i := 0; i < len(raw); i++ {
		if strings.HasPrefix(raw[i], "--") {
			key := strings.TrimPrefix(raw[i], "--")
			if i+1 < len(raw) && !strings.HasPrefix(raw[i+1], "--") {
				args[key] = raw[i+1]
				i++
			} else {
				args[key] = "true"
			}
		}
	}
	return args
}

func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func closestMatch(input string, candidates []string) string {
	best := ""
	bestDist := 3
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(input), strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `kuma-approve -- AI-agent CLI for Gmail, Calendar, and shell with Telegram approval

Usage:
  kuma-approve <service> <action> [flags]
  kuma-approve setup
  kuma-approve auth <service> <account>

Services:
  gmail       Gmail operations (list, get, search, send, reply, draft)
  gcal        Google Calendar operations (list, get, create, update, delete)
  outlook     Outlook email operations (list, get, search, send, reply, draft)
  msft-cal    Microsoft Calendar operations (list, get, create, update, delete)
  exec        Shell command execution (run)

Examples:
  kuma-approve gmail list --limit 10
  kuma-approve gcal create --title "Standup" --start 2026-09-05T09:00:00Z --end 2026-09-05T09:30:00Z
  kuma-approve exec run --cmd "df -h"

Run 'kuma-approve <service> --help' for actions, parameters, and examples.
Run 'kuma-approve setup' for first-time configuration.`)
}
