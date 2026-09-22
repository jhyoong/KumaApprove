package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/jhyoong/KumaApprove/internal/relay"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	handler := relay.NewHandler()

	fmt.Fprintf(os.Stderr, "kuma-relay listening on %s\n", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
