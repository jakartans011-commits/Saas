package auth

import (
	"log"
	"os"
)

type Mailer interface {
	SendVerificationEmail(toEmail, rawToken string) error
	SendPasswordResetEmail(toEmail, rawToken string) error
}

// LoggingMailer — placeholder aman sampai provider (SMTP/API) dipilih.
// Sama semangatnya dengan notify.LoggingSender: TIDAK mengirim email
// sungguhan, hanya mencatat subject+link ke log supaya bisa diverifikasi
// manual di dev/staging (checklist 2A.1). Isi email diambil dari
// email_templates.go — sama persis dengan yang akan dipakai ProdMailer
// nanti, supaya ganti provider tidak mengubah konten.
type LoggingMailer struct{ BaseURL string }

func (m *LoggingMailer) SendVerificationEmail(toEmail, rawToken string) error {
	subject, textBody, _ := BuildVerificationEmail(m.BaseURL, rawToken)
	log.Printf("notify: [NO SENDER CONFIGURED] to=%s subject=%q\n%s", toEmail, subject, textBody)
	return nil
}

func (m *LoggingMailer) SendPasswordResetEmail(toEmail, rawToken string) error {
	subject, textBody, _ := BuildPasswordResetEmail(m.BaseURL, rawToken)
	log.Printf("notify: [NO SENDER CONFIGURED] to=%s subject=%q\n%s", toEmail, subject, textBody)
	return nil
}

func NewMailerFromEnv() Mailer {
	baseURL := os.Getenv("APP_BASE_URL")
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		from := os.Getenv("NOTIFY_FROM_EMAIL")
		if from == "" {
			from = "TrailSeal <notifications@trailseal.dev>" // domain HARUS terverifikasi di Resend
		}
		return NewProdMailer(key, from, baseURL)
	}
	return &LoggingMailer{BaseURL: baseURL}
}
