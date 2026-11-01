// mcp/protocol_invalid_request_test.go
package mcp

import "testing"

func TestInvalidRequestError(t *testing.T) {
	err := invalidRequestError("something went wrong")

	if got := MCPErrorCodeOf(err); got != MCPErrorInvalidRequest {
		t.Fatalf("expected MCPErrorInvalidRequest, got %v", got)
	}
	if err.Error() != "something went wrong" {
		t.Fatalf("expected message 'something went wrong', got %q", err.Error())
	}
}