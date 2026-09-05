package main

import "testing"

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
