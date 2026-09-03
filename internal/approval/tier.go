package approval

// ResolveTier determines the approval tier for an action.
// It checks config overrides first, then falls back to the action's default tier.
// If neither is valid, it defaults to TierApprove (require approval).
func ResolveTier(actionKey, defaultTier string, overrides map[string]string) string {
	if overrides != nil {
		if tier, ok := overrides[actionKey]; ok && IsValidTier(tier) {
			return tier
		}
	}
	if IsValidTier(defaultTier) {
		return defaultTier
	}
	return TierApprove
}

// IsValidTier returns true if tier is one of the recognised tier values.
func IsValidTier(tier string) bool {
	return tier == TierAuto || tier == TierApprove || tier == TierDeny
}
