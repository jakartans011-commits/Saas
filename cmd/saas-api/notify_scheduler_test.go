package main

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"trailseal/notify"
)

type captureSender struct {
	jobs []*notify.EmailJob
}

func (c *captureSender) Send(job *notify.EmailJob) error {
	c.jobs = append(c.jobs, job)
	return nil
}

func TestDrainNotifyOnce_SendsQueuedJobAndClearsQueue(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "notify.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	queue, err := notify.NewBoltEmailQueue(db)
	if err != nil {
		t.Fatalf("NewBoltEmailQueue failed: %v", err)
	}

	enqueued, err := queue.EnqueueQuotaThreshold("tenant-notify-1", "", notify.TemplateQuota80, map[string]string{"percent": "80"})
	if err != nil || !enqueued {
		t.Fatalf("enqueue failed: enqueued=%v err=%v", enqueued, err)
	}

	sender := &captureSender{}
	sent, failed := drainNotifyOnce(queue, sender)

	if sent != 1 || failed != 0 {
		t.Fatalf("expected sent=1 failed=0, got sent=%d failed=%d", sent, failed)
	}
	if len(sender.jobs) != 1 || sender.jobs[0].TenantID != "tenant-notify-1" {
		t.Fatalf("sender did not receive the queued job: %+v", sender.jobs)
	}

	pending, err := queue.Pending(10)
	if err != nil {
		t.Fatalf("Pending failed: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("queue must be empty after a successful drain, got %d pending", len(pending))
	}
}

func TestDrainNotifyOnce_EmptyQueueIsNoop(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "notify-empty.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	queue, err := notify.NewBoltEmailQueue(db)
	if err != nil {
		t.Fatalf("NewBoltEmailQueue failed: %v", err)
	}

	sender := &captureSender{}
	sent, failed := drainNotifyOnce(queue, sender)
	if sent != 0 || failed != 0 || len(sender.jobs) != 0 {
		t.Fatalf("empty queue must be a no-op, got sent=%d failed=%d jobs=%d", sent, failed, len(sender.jobs))
	}
}
