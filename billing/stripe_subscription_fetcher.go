package billing

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/subscription"
)

// StripeSubscriptionFetcher — implementasi nyata SubscriptionFetcher.
//
// Dipisahkan dari stripe_reconcile.go supaya seluruh logika rekonsiliasi
// bisa diuji tanpa menyentuh jaringan maupun SDK Stripe. Berkas ini
// satu-satunya yang benar-benar berbicara dengan Stripe, dan karena itu
// sengaja dibuat setipis mungkin: ambil, terjemahkan, pulangkan.
//
// stripe.Key di-set di app.go dari STRIPE_SECRET_KEY, sama seperti jalur
// checkout dan portal.
type StripeSubscriptionFetcher struct{}

func NewStripeSubscriptionFetcher() *StripeSubscriptionFetcher {
	return &StripeSubscriptionFetcher{}
}

func (f *StripeSubscriptionFetcher) GetSubscription(
	ctx context.Context,
	subscriptionID string,
) (*ReconciledSubscription, error) {
	if subscriptionID == "" {
		return nil, fmt.Errorf("empty subscription id")
	}

	params := &stripe.SubscriptionParams{}
	params.Context = ctx

	sub, err := subscription.Get(subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe subscription %s: %w", subscriptionID, err)
	}

	out := &ReconciledSubscription{
		ID:     sub.ID,
		Status: string(sub.Status),
	}

	// Price bisa kosong pada subscription yang aneh. Membiarkannya kosong
	// membuat expectedPlanFor melewati tenant ini alih-alih menebak —
	// menebak di sini berarti menurunkan plan pelanggan yang membayar.
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		out.PriceID = sub.Items.Data[0].Price.ID
	}

	return out, nil
}
