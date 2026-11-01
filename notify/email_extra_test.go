package notify

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func openTestEmailQueueDB(t *testing.T) *BoltEmailQueue {
	t.Helper()
	path := filepath.Join(t.TempDir(), "email_extra_test.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	q, err := NewBoltEmailQueue(db)
	if err != nil {
		t.Fatalf("failed to init email queue: %v", err)
	}
	return q
}

// --- LoggingSender.Send ---

func TestLoggingSender_Send_NeverErrors(t *testing.T) {
	sender := LoggingSender{}
	job := &EmailJob{
		ID:       "job-1",
		TenantID: "tenant-a",
		Template: TemplateQuota80,
		Data:     map[string]string{"percent": "80"},
	}

	if err := sender.Send(job); err != nil {
		t.Fatalf("expected LoggingSender.Send to never error, got: %v", err)
	}
}

func TestLoggingSender_Send_NilData_NoPanic(t *testing.T) {
	sender := LoggingSender{}
	job := &EmailJob{ID: "job-2", TenantID: "tenant-a", Template: TemplateQuota100}

	if err := sender.Send(job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- SetQueue + package-level EnqueueQuotaThreshold ---

func TestPackageLevelEnqueueQuotaThreshold_NoopWhenQueueNotSet(t *testing.T) {
	// Ensure global state is nil before this test, and restore
	// whatever it was afterward so other tests aren't affected.
	SetQueue(nil)
	t.Cleanup(func() { SetQueue(nil) })

	// Must not panic even though no queue is configured.
	EnqueueQuotaThreshold("tenant-a", "project-a", TemplateQuota80, nil)
}

func TestSetQueueAndPackageLevelEnqueue_EnqueuesJob(t *testing.T) {
	q := openTestEmailQueueDB(t)
	SetQueue(q)
	t.Cleanup(func() { SetQueue(nil) })

	EnqueueQuotaThreshold("tenant-a", "project-a", TemplateQuota80, map[string]string{"percent": "80"})

	pending, err := q.Pending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending job after package-level enqueue, got %d", len(pending))
	}
	if pending[0].TenantID != "tenant-a" {
		t.Fatalf("expected tenant-a, got %s", pending[0].TenantID)
	}
}

func TestSetQueueAndPackageLevelEnqueue_DedupesSameTenantTemplatePeriod(t *testing.T) {
	q := openTestEmailQueueDB(t)
	SetQueue(q)
	t.Cleanup(func() { SetQueue(nil) })

	EnqueueQuotaThreshold("tenant-a", "project-a", TemplateQuota80, nil)
	EnqueueQuotaThreshold("tenant-a", "project-a", TemplateQuota80, nil)

	pending, err := q.Pending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected dedupe to keep only 1 pending job, got %d", len(pending))
	}
}
