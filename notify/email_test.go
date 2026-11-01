package notify

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func newTestQueue(t *testing.T) *BoltEmailQueue {
	t.Helper()

	dir := t.TempDir()
	db, err := bolt.Open(filepath.Join(dir, "notify_test.db"), 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	q, err := NewBoltEmailQueue(db)
	if err != nil {
		t.Fatalf("failed to init queue: %v", err)
	}
	return q
}

func TestEnqueueQuotaThreshold_DedupesWithinSamePeriod(t *testing.T) {
	q := newTestQueue(t)

	enqueued1, err := q.EnqueueQuotaThreshold("tenant-a", "proj-1", TemplateQuota80, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enqueued1 {
		t.Fatalf("expected first enqueue to succeed")
	}

	enqueued2, err := q.EnqueueQuotaThreshold("tenant-a", "proj-1", TemplateQuota80, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enqueued2 {
		t.Errorf("expected second enqueue for same tenant/template/period to be deduped")
	}

	pending, err := q.Pending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("expected exactly 1 pending job, got %d", len(pending))
	}
}

func TestEnqueueQuotaThreshold_DifferentTemplatesAreIndependent(t *testing.T) {
	q := newTestQueue(t)

	if _, err := q.EnqueueQuotaThreshold("tenant-a", "proj-1", TemplateQuota80, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := q.EnqueueQuotaThreshold("tenant-a", "proj-1", TemplateQuota100, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pending, err := q.Pending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 independent pending jobs (80%% and 100%%), got %d", len(pending))
	}
}

func TestEnqueueQuotaThreshold_EmptyTenantIsNoop(t *testing.T) {
	q := newTestQueue(t)

	enqueued, err := q.EnqueueQuotaThreshold("", "proj-1", TemplateQuota80, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enqueued {
		t.Errorf("expected empty tenant id to be a no-op")
	}
}

type fakeSender struct {
	sentJobs []*EmailJob
	failFor  map[string]bool
}

func (f *fakeSender) Send(job *EmailJob) error {
	if f.failFor[job.Template.String_ForTest()] {
		return errors.New("simulated send failure")
	}
	f.sentJobs = append(f.sentJobs, job)
	return nil
}

// String_ForTest — helper local ke test, hindari menambah method
// exported ke tipe Template hanya demi test ini.
func (t Template) String_ForTest() string { return string(t) }

func TestDrainOnce_MarksSentJobsAndKeepsFailedOnesPending(t *testing.T) {
	q := newTestQueue(t)

	if _, err := q.EnqueueQuotaThreshold("tenant-a", "proj-1", TemplateQuota80, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := q.EnqueueQuotaThreshold("tenant-b", "proj-2", TemplateQuota100, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sender := &fakeSender{failFor: map[string]bool{string(TemplateQuota100): true}}

	sent, failed, err := DrainOnce(q, sender, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent != 1 {
		t.Errorf("expected 1 sent job, got %d", sent)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed job, got %d", failed)
	}

	pending, err := q.Pending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 job still pending (the failed one), got %d", len(pending))
	}
	if pending[0].Template != TemplateQuota100 {
		t.Errorf("expected the still-pending job to be the quota_100 one, got %s", pending[0].Template)
	}
	if pending[0].Attempts != 1 {
		t.Errorf("expected failed job to have 1 recorded attempt, got %d", pending[0].Attempts)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}