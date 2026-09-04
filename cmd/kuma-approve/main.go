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

	// Resolve account.
	account := resolveAccount(args, cfg, serviceName, actionName)

	// Create service registry.
	registry := service.NewRegistry()
	registry.Register(gmail.New(gauth, account))
	registry.Register(gcal.New(gauth, account))

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
	setup.RunAuth(args[0], args[1])
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

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: kuma-approve <service> <action> [flags]

Services:
  gmail       Gmail operations
  gcal        Google Calendar operations
  exec        Shell command execution
  config      View/edit configuration
  auth        Manage OAuth authentication
  setup       First-time setup wizard

Run 'kuma-approve <service> --help' for service-specific help.`)
}
