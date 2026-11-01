package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestComputeHash_Deterministic(t *testing.T) {
	now := time.Now()
	payload := json.RawMessage(`{"action":"login"}`)

	h1 := ComputeHash("proj-1", 1, now, "user.login", "user-a", payload, "")
	h2 := ComputeHash("proj-1", 1, now, "user.login", "user-a", payload, "")

	if h1 != h2 {
		t.Fatal("expected identical inputs to produce identical hash")
	}
	t.Log("OK: hash is deterministic for identical inputs")
}

func TestComputeHash_DifferentPayloadDifferentHash(t *testing.T) {
	now := time.Now()

	h1 := ComputeHash("proj-1", 1, now, "user.login", "user-a", json.RawMessage(`{"ip":"1.1.1.1"}`), "")
	h2 := ComputeHash("proj-1", 1, now, "user.login", "user-a", json.RawMessage(`{"ip":"2.2.2.2"}`), "")

	if h1 == h2 {
		t.Fatal("expected different payloads to produce different hashes")
	}
	t.Log("OK: different payload changes the hash")
}

func TestVerifyChain_ValidChain(t *testing.T) {
	now := time.Now()

	e1Hash := ComputeHash("proj-1", 1, now, "user.created", "system", json.RawMessage(`{}`), "")
	e2Hash := ComputeHash("proj-1", 2, now, "user.login", "user-a", json.RawMessage(`{}`), e1Hash)

	chain := []*Event{
		{ID: "e1", ProjectID: "proj-1", Sequence: 1, EventType: "user.created", ActorID: "system", Payload: json.RawMessage(`{}`), OccurredAt: now, PrevHash: "", Hash: e1Hash},
		{ID: "e2", ProjectID: "proj-1", Sequence: 2, EventType: "user.login", ActorID: "user-a", Payload: json.RawMessage(`{}`), OccurredAt: now, PrevHash: e1Hash, Hash: e2Hash},
	}

	valid, brokenAt, err := VerifyChain(chain, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatalf("expected valid chain, got broken at %s", brokenAt)
	}
	t.Log("OK: valid 2-event chain verifies successfully")
}

func TestVerifyChain_DetectsTampering(t *testing.T) {
	now := time.Now()

	e1Hash := ComputeHash("proj-1", 1, now, "user.created", "system", json.RawMessage(`{}`), "")
	e2Hash := ComputeHash("proj-1", 2, now, "user.login", "user-a", json.RawMessage(`{}`), e1Hash)

	chain := []*Event{
		{ID: "e1", ProjectID: "proj-1", Sequence: 1, EventType: "user.created", ActorID: "system", Payload: json.RawMessage(`{}`), OccurredAt: now, PrevHash: "", Hash: e1Hash},
		// Payload event kedua DIUBAH setelah hash dihitung — simulasi tampering.
		{ID: "e2", ProjectID: "proj-1", Sequence: 2, EventType: "user.login", ActorID: "user-a", Payload: json.RawMessage(`{"tampered":true}`), OccurredAt: now, PrevHash: e1Hash, Hash: e2Hash},
	}

	valid, brokenAt, err := VerifyChain(chain, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("SECURITY FAILURE: tampered event was not detected")
	}
	if brokenAt != "e2" {
		t.Fatalf("expected break detected at e2, got %s", brokenAt)
	}
	t.Log("OK: tampered payload correctly detected at the right event")
}

func TestVerifyChain_ContinuesFromCheckpoint(t *testing.T) {
	now := time.Now()

	e1Hash := ComputeHash("proj-1", 1, now, "user.created", "system", json.RawMessage(`{}`), "")
	e2Hash := ComputeHash("proj-1", 2, now, "user.login", "user-a", json.RawMessage(`{}`), e1Hash)
	e3Hash := ComputeHash("proj-1", 3, now, "user.logout", "user-a", json.RawMessage(`{}`), e2Hash)

	// Hanya verifikasi event 3 dan seterusnya, mulai dari checkpoint
	// e2Hash — simulasi VerifyRange(fromSequence=3).
	chain := []*Event{
		{ID: "e3", ProjectID: "proj-1", Sequence: 3, EventType: "user.logout", ActorID: "user-a", Payload: json.RawMessage(`{}`), OccurredAt: now, PrevHash: e2Hash, Hash: e3Hash},
	}

	valid, brokenAt, err := VerifyChain(chain, e2Hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatalf("expected valid partial-chain verification from checkpoint, got broken at %s", brokenAt)
	}
	t.Log("OK: verification from a checkpoint (not from event 1) works correctly")
}

func TestIngestRequest_Validate(t *testing.T) {
	t.Run("empty_event_type_rejected", func(t *testing.T) {
		req := &IngestRequest{}
		if err := req.Validate(); err != ErrEventTypeRequired {
			t.Fatalf("expected ErrEventTypeRequired, got %v", err)
		}
	})

	t.Run("empty_payload_defaults_to_empty_object", func(t *testing.T) {
		req := &IngestRequest{EventType: "test.event"}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(req.Payload) != "{}" {
			t.Fatalf("expected payload to default to {}, got %s", req.Payload)
		}
	})
}
