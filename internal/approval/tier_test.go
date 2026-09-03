package approval

import "testing"

func TestResolveTierDefaultAuto(t *testing.T) {
	tier := ResolveTier("gmail:list", "auto", nil)
	if tier != TierAuto {
		t.Fatalf("expected auto, got %s", tier)
	}
}

func TestResolveTierDefaultApprove(t *testing.T) {
	tier := ResolveTier("gmail:send", "approve", nil)
	if tier != TierApprove {
		t.Fatalf("expected approve, got %s", tier)
	}
}

func TestResolveTierConfigOverride(t *testing.T) {
	overrides := map[string]string{
		"gmail:list": "approve",
	}
	tier := ResolveTier("gmail:list", "auto", overrides)
	if tier != TierApprove {
		t.Fatalf("expected approve from override, got %s", tier)
	}
}

func TestResolveTierDeny(t *testing.T) {
	overrides := map[string]string{
		"exec:run": "deny",
	}
	tier := ResolveTier("exec:run", "approve", overrides)
	if tier != TierDeny {
		t.Fatalf("expected deny, got %s", tier)
	}
}

func TestTierIsValid(t *testing.T) {
	cases := []struct {
		tier  string
		valid bool
	}{
		{"auto", true},
		{"approve", true},
		{"deny", true},
		{"invalid", false},
		{"", false},
	}
	for _, tc := range cases {
		if IsValidTier(tc.tier) != tc.valid {
			t.Fatalf("IsValidTier(%q) expected %v", tc.tier, tc.valid)
		}
	}
}
