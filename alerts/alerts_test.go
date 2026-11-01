package alerts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlackPayload_ContainsTitleMessageAndFields(t *testing.T) {
	payload := slackPayload("Chain broken", "Integrity check failed", map[string]string{
		"project_id": "proj-1",
		"event_id":   "evt-9",
	})

	text, ok := payload["text"].(string)
	if !ok {
		t.Fatalf("expected text field to be a string, got %T", payload["text"])
	}

	for _, want := range []string{"Chain broken", "Integrity check failed", "proj-1", "evt-9"} {
		if !contains(text, want) {
			t.Errorf("expected slack text to contain %q, got: %s", want, text)
		}
	}
}

func TestDiscordPayload_HasEmbedWithTitleAndDescription(t *testing.T) {
	payload := discordPayload("Quota exceeded", "Project hit its monthly limit", map[string]string{
		"project_id": "proj-2",
	})

	embeds, ok := payload["embeds"].([]map[string]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected exactly one embed, got %#v", payload["embeds"])
	}

	if embeds[0]["title"] != "Quota exceeded" {
		t.Errorf("unexpected embed title: %v", embeds[0]["title"])
	}
	if !contains(embeds[0]["description"].(string), "proj-2") {
		t.Errorf("expected embed description to contain field, got: %v", embeds[0]["description"])
	}
}

func TestNotifyCritical_RespectsCooldownPerDedupeKey(t *testing.T) {
	var callCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	InitWithConfig(server.URL, "slack")

	mu.Lock()
	current.cooldown = 50 * time.Millisecond
	mu.Unlock()

	NotifyCritical("quota_exceeded:proj-1", "Quota exceeded", "first", nil)
	NotifyCritical("quota_exceeded:proj-1", "Quota exceeded", "second (should be suppressed)", nil)

	// Different dedupe key must NOT be suppressed by proj-1's cooldown.
	NotifyCritical("quota_exceeded:proj-2", "Quota exceeded", "different project", nil)

	// Give the fire-and-forget goroutines a moment to land.
	time.Sleep(150 * time.Millisecond)

	got := atomic.LoadInt32(&callCount)
	if got != 2 {
		t.Errorf("expected 2 webhook deliveries (dedup + different key), got %d", got)
	}
}

func TestNotifyCritical_NoopWhenNotConfigured(t *testing.T) {
	InitWithConfig("", "")

	// Must not panic and must not block — no assertions beyond "returns".
	NotifyCritical("some_key", "title", "message", map[string]string{"a": "b"})
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
