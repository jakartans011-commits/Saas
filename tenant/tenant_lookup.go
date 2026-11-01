package tenant

// TenantByStripeLookup adalah interface terpisah untuk mencari tenant
// dari Stripe Customer ID. InMemoryTenantStore dan BoltTenantStore
// sudah memenuhi ini tanpa perubahan apapun (method FindByStripeCustomerID
// sudah ada di keduanya).
type TenantByStripeLookup interface {
	FindByStripeCustomerID(customerID string) (*Tenant, error)
}
