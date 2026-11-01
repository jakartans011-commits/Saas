package mcp

import (
	"regexp"
	"testing"
)

// uuidPattern matches a canonical (RFC 4122) UUID string, e.g. what
// uuid.New().String() produces: 8-4-4-4-12 hex digits.
var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

// TestNewRequestID_ReturnsValidUUID verifies newRequestID produces a
// well-formed UUID string, since that string is what ends up in
// MCPResponse.Metadata["request_id"] and MCPExecutionContext.RequestID.
func TestNewRequestID_ReturnsValidUUID(t *testing.T) {
	id := newRequestID()

	if !uuidPattern.MatchString(id) {
		t.Errorf("expected newRequestID() to return a UUID string, got %q", id)
	}
}

// TestNewRequestID_IsUniquePerCall verifies each call produces a
// distinct ID, so concurrent/sequential requests remain traceable
// independently.
func TestNewRequestID_IsUniquePerCall(t *testing.T) {
	seen := make(map[string]bool)

	const iterations = 1000

	for i := 0; i < iterations; i++ {
		id := newRequestID()

		if seen[id] {
			t.Fatalf("newRequestID() produced a duplicate on iteration %d: %q", i, id)
		}

		seen[id] = true
	}

	if len(seen) != iterations {
		t.Fatalf("expected %d unique ids, got %d", iterations, len(seen))
	}
}

// TestNewRequestID_NotEmpty guards against a degenerate implementation
// that would silently satisfy other checks with a constant non-UUID
// value.
func TestNewRequestID_NotEmpty(t *testing.T) {
	if newRequestID() == "" {
		t.Fatal("expected newRequestID() to return a non-empty string")
	}
}