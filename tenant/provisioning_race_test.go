package tenant

import (
	"errors"
	"sync"
	"testing"

	"trailseal/auth"
	"trailseal/rbac"
)

// ============================================================
// FAKE ROLE ADMIN STORE (implements rbac.RoleAdminStore)
// ============================================================

type fakeRoleAdminStore struct {
	mu    sync.Mutex
	roles map[string]*rbac.Role // key: tenantID + "|" + roleID
}

func newFakeRoleAdminStore() *fakeRoleAdminStore {
	return &fakeRoleAdminStore{roles: make(map[string]*rbac.Role)}
}

func roleKey(tenantID, roleID string) string { return tenantID + "|" + roleID }

func (s *fakeRoleAdminStore) GetRole(tenantID, roleID string) (*rbac.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roles[roleKey(tenantID, roleID)]
	if !ok {
		return nil, errors.New("role_not_found")
	}
	return r, nil
}

func (s *fakeRoleAdminStore) PutRole(role *rbac.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[roleKey(role.TenantID, role.ID)] = role
	return nil
}

func (s *fakeRoleAdminStore) DeleteAllForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, r := range s.roles {
		if r.TenantID == tenantID {
			delete(s.roles, k)
			count++
		}
	}
	return count
}

func (s *fakeRoleAdminStore) CountForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, r := range s.roles {
		if r.TenantID == tenantID {
			count++
		}
	}
	return count
}

// failingPutRoleStore wraps fakeRoleAdminStore but forces PutRole to
// fail, so we can test ProvisionTenant's rollback path deterministically.
type failingPutRoleStore struct {
	*fakeRoleAdminStore
}

func (s *failingPutRoleStore) PutRole(role *rbac.Role) error {
	return errors.New("simulated_role_store_failure")
}

// ============================================================
// FAKE USER ADMIN STORE (implements auth.UserAdminStore)
// ============================================================

type fakeUserAdminStore struct {
	mu    sync.Mutex
	users map[string]*auth.User
}

func newFakeUserAdminStore() *fakeUserAdminStore {
	return &fakeUserAdminStore{users: make(map[string]*auth.User)}
}

func (s *fakeUserAdminStore) GetByID(userID string) (*auth.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	return u, nil
}

func (s *fakeUserAdminStore) Put(user *auth.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID] = user
	return nil
}

func (s *fakeUserAdminStore) DeleteAllForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, u := range s.users {
		if u.TenantID == tenantID {
			delete(s.users, k)
			count++
		}
	}
	return count
}

func (s *fakeUserAdminStore) CountForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, u := range s.users {
		if u.TenantID == tenantID {
			count++
		}
	}
	return count
}

// ============================================================
// TEST 1 — RACE: N goroutines provisioning the SAME tenant ID
// concurrently must result in exactly ONE success and exactly
// ONE owner role created. Run with `go test -race` to also catch
// any data-race the atomic fix might have missed.
// ============================================================

func TestProvisionTenant_ConcurrentSameID_OnlyOneWins(t *testing.T) {
	tenantStore := NewInMemoryTenantStore()
	roleStore := newFakeRoleAdminStore()

	const attempts = 20
	var wg sync.WaitGroup
	successCount := 0
	alreadyExistsCount := 0
	otherErrCount := 0
	var mu sync.Mutex

	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			_, err := ProvisionTenant(tenantStore, roleStore, "tenant-race", "Race Co")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successCount++
			case errors.Is(err, ErrTenantAlreadyExists):
				alreadyExistsCount++
			default:
				otherErrCount++
				t.Logf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if alreadyExistsCount != attempts-1 {
		t.Errorf("expected %d ErrTenantAlreadyExists, got %d", attempts-1, alreadyExistsCount)
	}
	if otherErrCount != 0 {
		t.Errorf("expected 0 unexpected errors, got %d", otherErrCount)
	}

	// The decisive assertion: exactly ONE owner role must exist for
	// this tenant. Before the PutIfNotExists fix, a lost-update race
	// on Put could let two goroutines both pass the Put step and both
	// write an owner role — this catches that even if the tenant
	// record itself looked fine.
	if got := roleStore.CountForTenant("tenant-race"); got != 1 {
		t.Errorf("expected exactly 1 owner role for tenant-race, got %d", got)
	}

	tenant, err := tenantStore.Get("tenant-race")
	if err != nil {
		t.Fatalf("expected tenant to exist after provisioning, got error: %v", err)
	}
	if tenant.Status != TenantStatusActive {
		t.Errorf("expected tenant status active, got %s", tenant.Status)
	}
}

// ============================================================
// TEST 2 — Basic non-race sanity check: provisioning the same
// tenant ID twice sequentially must reject the second call.
// ============================================================

func TestProvisionTenant_TenantAlreadyExists_Rejected(t *testing.T) {
	tenantStore := NewInMemoryTenantStore()
	roleStore := newFakeRoleAdminStore()

	_, err := ProvisionTenant(tenantStore, roleStore, "tenant-1", "Acme")
	if err != nil {
		t.Fatalf("first provision should succeed, got: %v", err)
	}

	_, err = ProvisionTenant(tenantStore, roleStore, "tenant-1", "Acme Again")
	if !errors.Is(err, ErrTenantAlreadyExists) {
		t.Errorf("expected ErrTenantAlreadyExists, got: %v", err)
	}

	if got := roleStore.CountForTenant("tenant-1"); got != 1 {
		t.Errorf("expected exactly 1 owner role after rejected re-provision, got %d", got)
	}
}

// ============================================================
// TEST 3 — Rollback path: if role creation fails after the tenant
// record was written, the tenant must be marked deprovisioned, not
// left "active" with no owner role (the "ghost tenant" scenario).
// ============================================================

func TestProvisionTenant_RoleCreationFails_TenantMarkedDeprovisioned(t *testing.T) {
	tenantStore := NewInMemoryTenantStore()
	roleStore := &failingPutRoleStore{fakeRoleAdminStore: newFakeRoleAdminStore()}

	_, err := ProvisionTenant(tenantStore, roleStore, "tenant-fail", "Failco")
	if err == nil {
		t.Fatal("expected error when role creation fails, got nil")
	}

	// Get() only returns non-deprovisioned... actually Get() filters
	// soft-deleted (DeletedAt), not Status, so it WILL return the
	// tenant here — check its Status explicitly.
	tenant, getErr := tenantStore.Get("tenant-fail")
	if getErr != nil {
		t.Fatalf("expected tenant record to still exist after rollback, got error: %v", getErr)
	}
	if tenant.Status != TenantStatusDeprovisioned {
		t.Errorf("expected tenant marked deprovisioned after role failure, got status: %s", tenant.Status)
	}

	if got := roleStore.CountForTenant("tenant-fail"); got != 0 {
		t.Errorf("expected 0 roles for tenant-fail after rollback, got %d", got)
	}
}

// ============================================================
// TEST 4 — DeprovisionTenant idempotency: calling it twice must
// not corrupt state or double-count deletions on the second call.
// ============================================================

func TestDeprovisionTenant_Idempotent(t *testing.T) {
	tenantStore := NewInMemoryTenantStore()
	roleStore := newFakeRoleAdminStore()
	userStore := newFakeUserAdminStore()

	if _, err := ProvisionTenant(tenantStore, roleStore, "tenant-dep", "DepCo"); err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	_ = userStore.Put(&auth.User{ID: "u1", TenantID: "tenant-dep", Status: auth.UserStatusActive})
	_ = userStore.Put(&auth.User{ID: "u2", TenantID: "tenant-dep", Status: auth.UserStatusActive})

	result1, err := DeprovisionTenant(tenantStore, roleStore, userStore, "tenant-dep")
	if err != nil {
		t.Fatalf("first deprovision failed: %v", err)
	}
	if result1.UsersDeleted != 2 {
		t.Errorf("expected 2 users deleted on first call, got %d", result1.UsersDeleted)
	}
	if result1.RolesDeleted != 1 {
		t.Errorf("expected 1 role deleted on first call, got %d", result1.RolesDeleted)
	}

	// Second call must be rejected cleanly, not silently re-run cleanup.
	_, err = DeprovisionTenant(tenantStore, roleStore, userStore, "tenant-dep")
	if err == nil {
		t.Fatal("expected error on second deprovision of already-deprovisioned tenant, got nil")
	}

	// Confirm no negative/duplicate counts leaked from a second cleanup pass.
	if got := userStore.CountForTenant("tenant-dep"); got != 0 {
		t.Errorf("expected 0 users remaining, got %d", got)
	}
	if got := roleStore.CountForTenant("tenant-dep"); got != 0 {
		t.Errorf("expected 0 roles remaining, got %d", got)
	}
}