package billing

// PlanAllowsTeamSeats: hanya Pro dan Business yang boleh punya anggota tim
// (admin/member). PlanTier kosong atau tidak dikenal dianggap Free (fail closed),
// sama dengan planResolver di app.go dan provisionRolesForPlan.
func PlanAllowsTeamSeats(planTier string) bool {
	tier := PlanTier(planTier)
	return tier == PlanPro || tier == PlanBusiness
}
