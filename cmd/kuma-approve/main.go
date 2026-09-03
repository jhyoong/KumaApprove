package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/cli"
	"github.com/jhyoong/KumaApprove/internal/output"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	service := os.Args[1]

	if service == "help" || service == "--help" || service == "-h" {
		printUsage()
		return
	}

	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: kuma-approve %s <action> [flags]\n", service)
		os.Exit(1)
	}

	action := os.Args[2]
	args := parseFlags(os.Args[3:])

	router := cli.NewRouter()
	// Services will be registered here as they are implemented

	result, err := router.Dispatch(service, action, args)
	if err != nil {
		output.PrintAndExit(output.Fail(
			service+":"+action,
			"INVALID_ARGS",
			err.Error(),
		))
		return
	}

	output.PrintAndExit(output.Success(service+":"+action, result))
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
