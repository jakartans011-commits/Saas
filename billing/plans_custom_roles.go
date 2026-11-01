package billing

// PlanAllowsCustomRoles membaca flag CustomRoles dari definisi plan (sumber
// kebenaran tunggal, sama dengan tabel pricing). PlanTier kosong atau tidak
// dikenal dianggap Free (fail closed).
func PlanAllowsCustomRoles(planTier string) bool {
	c, ok := baseDefinitions[PlanTier(planTier)]
	return ok && c.CustomRoles
}