package alerts

import (
	"testing"
)

func TestEnabled_FalseWhenNotConfigured(t *testing.T) {
	InitWithConfig("", "")
	if Enabled() {
		t.Fatal("expected Enabled() to be false when no webhook URL is configured")
	}
}

func TestEnabled_TrueWhenWebhookConfigured(t *testing.T) {
	InitWithConfig("https://hooks.example.com/webhook", "slack")
	if !Enabled() {
		t.Fatal("expected Enabled() to be true when a webhook URL is configured")
	}
}

func TestEnabled_TrueForDiscordProvider(t *testing.T) {
	InitWithConfig("https://discord.example.com/webhook", "discord")
	if !Enabled() {
		t.Fatal("expected Enabled() to be true for a configured discord webhook")
	}
}

func TestInit_ReadsFromEnvironment_Disabled(t *testing.T) {
	t.Setenv("ALERT_WEBHOOK_URL", "")
	t.Setenv("ALERT_WEBHOOK_PROVIDER", "")
	t.Setenv("ALERT_COOLDOWN_SECONDS", "")

	Init()

	if Enabled() {
		t.Fatal("expected Enabled() to be false after Init() with no env vars set")
	}
}

func TestInit_ReadsFromEnvironment_Enabled(t *testing.T) {
	t.Setenv("ALERT_WEBHOOK_URL", "https://hooks.example.com/from-env")
	t.Setenv("ALERT_WEBHOOK_PROVIDER", "slack")

	Init()

	if !Enabled() {
		t.Fatal("expected Enabled() to be true after Init() with ALERT_WEBHOOK_URL set")
	}

	// Reset global state so this test doesn't leak into others.
	InitWithConfig("", "")
}

func TestInit_ReadsCooldownFromEnvironment(t *testing.T) {
	t.Setenv("ALERT_WEBHOOK_URL", "https://hooks.example.com/cooldown-test")
	t.Setenv("ALERT_WEBHOOK_PROVIDER", "slack")
	t.Setenv("ALERT_COOLDOWN_SECONDS", "60")

	Init()

	mu.RLock()
	n := current
	mu.RUnlock()

	if n == nil {
		t.Fatal("expected notifier to be configured")
	}
	if n.cooldown.Seconds() != 60 {
		t.Fatalf("expected cooldown of 60s from env, got %v", n.cooldown)
	}

	InitWithConfig("", "")
}
