package auth

// ListByTenant returns all non-deleted users belonging to tenantID.
// Mirrors CountForTenant's iteration pattern (see user_store_admin.go) —
// kept as a separate method rather than reusing Count's loop so callers
// needing the full objects don't pay for building a slice they discard.
func (s *InMemoryUserStore) ListByTenant(tenantID string) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var users []*User
	for _, user := range s.users {
		if user.TenantID != tenantID {
			continue
		}
		// Soft-deleted users are not listed, same convention as GetByID.
		if user.DeletedAt != nil {
			continue
		}
		users = append(users, user)
	}
	return users
}
