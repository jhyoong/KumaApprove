package main

import (
	"testing"

	"github.com/jhyoong/KumaApprove/internal/config"
)

func TestClosestMatchPrefix(t *testing.T) {
	services := []string{"gmail", "gcal", "outlook", "msft-cal", "exec"}

	tests := []struct {
		input string
		want  string
	}{
		{"msft", "msft-cal"},
		{"outloo", "outlook"},
		{"gmai", "gmail"},
		{"xyz", ""},
		{"gm", ""},
		{"ex", "exec"},
		{"exe", "exec"},
		{"msft-cal", "msft-cal"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := closestMatch(tt.input, services)
			if got != tt.want {
				t.Fatalf("closestMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplyTrace(t *testing.T) {
	cfg := config.Default()

	if err := applyTrace(&cfg, "on"); err != nil || !cfg.Approval.Trace {
		t.Fatalf("expected trace on, got trace=%v err=%v", cfg.Approval.Trace, err)
	}
	if err := applyTrace(&cfg, "status"); err != nil || !cfg.Approval.Trace {
		t.Fatalf("expected status to leave trace on, got trace=%v err=%v", cfg.Approval.Trace, err)
	}
	if err := applyTrace(&cfg, "off"); err != nil || cfg.Approval.Trace {
		t.Fatalf("expected trace off, got trace=%v err=%v", cfg.Approval.Trace, err)
	}
	if err := applyTrace(&cfg, "maybe"); err == nil {
		t.Fatal("expected an unknown argument to be rejected")
	}
}
