package main

import "context"

// tenantQuotaReader membaca pemakaian kuota event dari sumber yang SAMA
// dengan yang dipakai ingest untuk menolak dengan quota_exceeded.
//
// Didefinisikan di berkas tersendiri supaya usage_handlers.go tidak perlu
// menambah import apa pun. *events.PostgresStore memenuhinya apa adanya.
type tenantQuotaReader interface {
	TenantQuotaUsage(ctx context.Context, tenantID string) (used int64, limit int64, err error)
}
