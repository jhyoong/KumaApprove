package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kuma-approve <service> <action> [flags]")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "not yet implemented")
	os.Exit(1)
}
