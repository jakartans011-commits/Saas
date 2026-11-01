// notify/resend_sender.go
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ResendSender implements EmailSender using the Resend REST API
// (https://resend.com/docs/api-reference/emails/send-email).
// No SDK dependency — plain net/http keeps this in line with the
// rest of the codebase's minimal-dependency style.
type ResendSender struct {
	APIKey    string
	From      string                                // e.g. "TrailSeal <alerts@yourdomain.com>"
	Resolve   func(tenantID string) (string, error) // resolves recipient email for a tenant
	Templates map[Template]func(job *EmailJob) (subject, html string)
	client    *http.Client
}

func NewResendSender(apiKey, from string, resolve func(tenantID string) (string, error)) *ResendSender {
	return &ResendSender{
		APIKey:    apiKey,
		From:      from,
		Resolve:   resolve,
		Templates: defaultTemplates(),
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Html    string   `json:"html"`
}

func (s *ResendSender) Send(job *EmailJob) error {
	to := job.To
	if to == "" {
		if s.Resolve == nil {
			return fmt.Errorf("resend: no recipient on job and no Resolve func configured")
		}
		resolved, err := s.Resolve(job.TenantID)
		if err != nil {
			return fmt.Errorf("resend: failed to resolve recipient for tenant %s: %w", job.TenantID, err)
		}
		to = resolved
	}
	if to == "" {
		return fmt.Errorf("resend: empty recipient for tenant %s, template %s", job.TenantID, job.Template)
	}

	renderFn, ok := s.Templates[job.Template]
	if !ok {
		return fmt.Errorf("resend: no template renderer registered for %s", job.Template)
	}
	subject, html := renderFn(job)

	payload := resendPayload{
		From:    s.From,
		To:      []string{to},
		Subject: subject,
		Html:    html,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend: failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend: API returned status %d for tenant %s template %s", resp.StatusCode, job.TenantID, job.Template)
	}
	return nil
}

// defaultTemplates — minimal inline HTML, no external template engine.
// Swap these out for nicer branded templates later; the Send() logic
// above never needs to change.
func defaultTemplates() map[Template]func(job *EmailJob) (string, string) {
	return map[Template]func(job *EmailJob) (string, string){
		TemplateQuota80: func(job *EmailJob) (string, string) {
			return "TrailSeal: you're at 80% of your monthly quota",
				fmt.Sprintf("<p>Your tenant <b>%s</b> has used 80%% of its monthly event quota. "+
					"Consider upgrading your plan to avoid interruption.</p>", job.TenantID)
		},
		TemplateQuota100: func(job *EmailJob) (string, string) {
			return "TrailSeal: you've reached 100% of your monthly quota",
				fmt.Sprintf("<p>Your tenant <b>%s</b> has reached its monthly event quota. "+
					"New events may be rejected until next billing period or upgrade.</p>", job.TenantID)
		},
		TemplateChainBroken: func(job *EmailJob) (string, string) {
			eventID := job.Data["event_id"]
			return "TrailSeal: Chain Integrity check failed",
				fmt.Sprintf("<p>Chain verification for tenant <b>%s</b> failed starting at event <code>%s</code>. "+
					"Please review the Chain Integrity panel in your dashboard.</p>", job.TenantID, eventID)
		},
	}
}
