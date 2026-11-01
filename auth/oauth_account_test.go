package auth

import (
	"errors"
	"testing"
)

// --- mocks ringan, implement UserEmailLookupStore + UserPutStore ---

type mockOAuthUserStore struct {
	byEmail  map[string]*User
	putCalls []*User
}

func newMockOAuthUserStore() *mockOAuthUserStore {
	return &mockOAuthUserStore{byEmail: map[string]*User{}}
}

func (m *mockOAuthUserStore) FindByEmail(email string) (*User, error) {
	if u, ok := m.byEmail[email]; ok {
		return u, nil
	}
	return nil, errors.New("user_not_found")
}

func (m *mockOAuthUserStore) Put(user *User) error {
	m.putCalls = append(m.putCalls, user)
	m.byEmail[user.Email] = user
	return nil
}

func stubNewTenant(tenantID, ownerRoleID string, err error) NewTenantFunc {
	return func(ownerEmail string) (string, string, error) {
		return tenantID, ownerRoleID, err
	}
}

func TestResolveOAuthUser_EmptyEmail_Rejected(t *testing.T) {
	store := newMockOAuthUserStore()
	_, err := ResolveOAuthUser(store, store, stubNewTenant("t1", "owner", nil), "")
	if !errors.Is(err, ErrOAuthEmailNotVerified) {
		t.Fatalf("expected ErrOAuthEmailNotVerified, got %v", err)
	}
}

func TestResolveOAuthUser_ExistingActiveUser_AutoLinked_NoNewTenantCalled(t *testing.T) {
	store := newMockOAuthUserStore()
	existing := &User{ID: "u1", Email: "john@example.com", TenantID: "t-old", Status: UserStatusActive, PasswordHash: "somehash"}
	store.byEmail[existing.Email] = existing

	newTenantCalled := false
	newTenant := func(ownerEmail string) (string, string, error) {
		newTenantCalled = true
		return "should-not-be-used", "owner", nil
	}

	got, err := ResolveOAuthUser(store, store, newTenant, "john@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected existing user to be returned as-is, got %+v", got)
	}
	if newTenantCalled {
		t.Fatal("newTenant must not be called when user already exists (auto-link path)")
	}
	if len(store.putCalls) != 0 {
		t.Fatal("Put must not be called when auto-linking an existing user")
	}
}

func TestResolveOAuthUser_ExistingSuspendedUser_Rejected(t *testing.T) {
	store := newMockOAuthUserStore()
	store.byEmail["banned@example.com"] = &User{
		ID: "u2", Email: "banned@example.com", Status: UserStatusSuspended,
	}

	_, err := ResolveOAuthUser(store, store, stubNewTenant("t1", "owner", nil), "banned@example.com")
	if !errors.Is(err, ErrUserSuspended) {
		t.Fatalf("expected ErrUserSuspended, got %v", err)
	}
}

func TestResolveOAuthUser_NewUser_CreatedWithEmptyPasswordHash(t *testing.T) {
	store := newMockOAuthUserStore()

	got, err := ResolveOAuthUser(store, store, stubNewTenant("t-new", "owner", nil), "fresh@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PasswordHash != "" {
		t.Fatalf("OAuth-only user must have empty PasswordHash, got %q", got.PasswordHash)
	}
	if got.TenantID != "t-new" || got.RoleID != "owner" {
		t.Fatalf("expected tenant/role from newTenant(), got tenant=%q role=%q", got.TenantID, got.RoleID)
	}
	if got.Status != UserStatusActive {
		t.Fatalf("expected new OAuth user to be active, got %q", got.Status)
	}
	if got.EmailVerifiedAt == nil {
		t.Fatal("expected EmailVerifiedAt to be set for OAuth-resolved user")
	}
	if len(store.putCalls) != 1 {
		t.Fatalf("expected exactly 1 Put call for new user, got %d", len(store.putCalls))
	}
}

func TestResolveOAuthUser_NewTenantFails_PropagatesError(t *testing.T) {
	store := newMockOAuthUserStore()
	wantErr := errors.New("provision_failed")

	_, err := ResolveOAuthUser(store, store, stubNewTenant("", "", wantErr), "noone@example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected newTenant error to propagate, got %v", err)
	}
	if len(store.putCalls) != 0 {
		t.Fatal("Put must not be called when tenant provisioning fails")
	}
}
