package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"trailseal/alerts"
)

// ProdMailer mengirim email auth lewat Resend. Pengiriman async: request
// signup/resend/forgot tidak menunggu provider, dan waktu respons tidak
// membedakan email terdaftar vs tidak (anti user-enumeration). Semua
// pemanggil memang mengabaikan error (akun tetap dibuat, user bisa resend).
//
// Kegagalan TIDAK boleh berhenti di log. Layar pelanggan sudah terlanjur
// mengatakan "email sedang dikirim", jadi selama pengiriman gagal mereka
// menunggu kotak masuk yang tidak akan pernah terisi — dan karena verifikasi
// email wajib untuk login, tidak ada satu pun pelanggan baru yang bisa masuk.
// Itu kegagalan diam yang harus membangunkan pemilik, bukan mengendap.
type ProdMailer struct {
	APIKey  string
	From    string
	BaseURL string
	client  *http.Client
}

func NewProdMailer(apiKey, from, baseURL string) *ProdMailer {
	return &ProdMailer{APIKey: apiKey, From: from, BaseURL: baseURL,
		client: &http.Client{Timeout: 10 * time.Second}}
}

type resendAuthPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// fail mencatat kegagalan sekaligus memicu alert. Kunci dedupe memakai kind,
// bukan alamat penerima, supaya badai kegagalan menjadi satu notifikasi
// (alerts punya cooldown sendiri) alih-alih satu per pendaftar.
func (m *ProdMailer) fail(kind, reason string) {
	log.Printf("ERROR: mailer: %s: %s", kind, reason)
	alerts.NotifyCritical(
		"mailer_failed:"+kind,
		"Email autentikasi gagal terkirim",
		"Pengiriman email gagal. Selama ini berlangsung, pendaftar baru tidak "+
			"menerima tautan verifikasi dan TIDAK BISA login, sementara layar "+
			"mereka tetap mengatakan email sudah dikirim.",
		map[string]string{
			"kind":   kind,
			"reason": reason,
		},
	)
}

func (m *ProdMailer) deliver(kind, to, subject, text, html string) {
	body, err := json.Marshal(resendAuthPayload{
		From: m.From, To: []string{to}, Subject: subject, HTML: html, Text: text,
	})
	if err != nil {
		m.fail(kind, "marshal: "+err.Error())
		return
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.resend.com/emails",
		bytes.NewReader(body),
	)
	if err != nil {
		m.fail(kind, "build request: "+err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		m.fail(kind, "request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Resend menjelaskan sebabnya di body — misalnya domain pengirim
		// belum diverifikasi. Tanpa ini yang terlihat hanya angka status
		// dan penyebabnya harus ditebak.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		reason := "resend status " + strconv.Itoa(resp.StatusCode)
		if s := strings.TrimSpace(string(snippet)); s != "" {
			reason += ": " + s
		}
		m.fail(kind, reason)
	}
}

func (m *ProdMailer) SendVerificationEmail(toEmail, rawToken string) error {
	subject, text, html := BuildVerificationEmail(m.BaseURL, rawToken)
	go m.deliver("verification", toEmail, subject, text, html)
	return nil
}

func (m *ProdMailer) SendPasswordResetEmail(toEmail, rawToken string) error {
	subject, text, html := BuildPasswordResetEmail(m.BaseURL, rawToken)
	go m.deliver("password_reset", toEmail, subject, text, html)
	return nil
}
