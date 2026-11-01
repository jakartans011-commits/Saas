package auth

import "fmt"

// ============================================================
// EMAIL TEMPLATES — 2A.2 (email verification)
//
// Dipisah dari Mailer interface (mailer.go) supaya:
//   - LoggingMailer (dev) dan ProdMailer (nanti, setelah provider
//     dipilih) sama-sama pakai isi email yang SAMA persis.
//   - Ganti provider TIDAK perlu tulis ulang subject/body.
//
// Setiap fungsi return (subject, textBody, htmlBody).
// htmlBody sengaja simpel (inline style, no external CSS/images)
// supaya render konsisten di semua email client.
// ============================================================

// BuildVerificationEmail — dipakai oleh SendVerificationEmail.
func BuildVerificationEmail(baseURL, rawToken string) (subject, textBody, htmlBody string) {
	link := fmt.Sprintf("%s/verify-email?token=%s", baseURL, rawToken)

	subject = "Verify your TrailSeal account"

	textBody = fmt.Sprintf(
		"Welcome to TrailSeal.\n\n"+
			"Please verify your email address by clicking the link below:\n\n"+
			"%s\n\n"+
			"This link expires in 24 hours. If you didn't create a TrailSeal account, you can safely ignore this email.\n\n"+
			"— TrailSeal",
		link,
	)

	htmlBody = fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#f5f5f5;padding:32px 0;margin:0;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0">
    <tr>
      <td align="center">
        <table role="presentation" width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:32px;">
          <tr>
            <td>
              <div style="font-weight:700;font-size:18px;color:#111;margin-bottom:24px;">TrailSeal</div>
              <h1 style="font-size:20px;color:#111;margin:0 0 12px;">Verify your email</h1>
              <p style="font-size:14px;color:#444;line-height:1.5;margin:0 0 24px;">
                Welcome to TrailSeal. Click the button below to verify your email address and activate your account.
              </p>
              <a href="%s" style="display:inline-block;background:#2952e3;color:#ffffff;text-decoration:none;padding:12px 24px;border-radius:6px;font-size:14px;font-weight:600;">
                Verify email address
              </a>
              <p style="font-size:12px;color:#888;line-height:1.5;margin:24px 0 0;">
                This link expires in 24 hours. If you didn't create a TrailSeal account, you can safely ignore this email.
              </p>
              <p style="font-size:12px;color:#aaa;margin:16px 0 0;word-break:break-all;">
                Or copy this link: %s
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, link, link)

	return subject, textBody, htmlBody
}

// BuildPasswordResetEmail — dipakai oleh SendPasswordResetEmail (2A.3).
// Disertakan di sini sekalian karena pola & struktur identik dengan
// verification email; hindari duplikasi kalau 2A.3 template dikerjakan
// menyusul.
func BuildPasswordResetEmail(baseURL, rawToken string) (subject, textBody, htmlBody string) {
	link := fmt.Sprintf("%s/reset-password?token=%s", baseURL, rawToken)

	subject = "Reset your TrailSeal password"

	textBody = fmt.Sprintf(
		"We received a request to reset your TrailSeal password.\n\n"+
			"Click the link below to choose a new password:\n\n"+
			"%s\n\n"+
			"This link expires in 1 hour. If you didn't request this, you can safely ignore this email — your password will not be changed.\n\n"+
			"— TrailSeal",
		link,
	)

	htmlBody = fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#f5f5f5;padding:32px 0;margin:0;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0">
    <tr>
      <td align="center">
        <table role="presentation" width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:32px;">
          <tr>
            <td>
              <div style="font-weight:700;font-size:18px;color:#111;margin-bottom:24px;">TrailSeal</div>
              <h1 style="font-size:20px;color:#111;margin:0 0 12px;">Reset your password</h1>
              <p style="font-size:14px;color:#444;line-height:1.5;margin:0 0 24px;">
                We received a request to reset your TrailSeal password. Click the button below to choose a new one.
              </p>
              <a href="%s" style="display:inline-block;background:#2952e3;color:#ffffff;text-decoration:none;padding:12px 24px;border-radius:6px;font-size:14px;font-weight:600;">
                Reset password
              </a>
              <p style="font-size:12px;color:#888;line-height:1.5;margin:24px 0 0;">
                This link expires in 1 hour. If you didn't request this, you can safely ignore this email — your password will not be changed.
              </p>
              <p style="font-size:12px;color:#aaa;margin:16px 0 0;word-break:break-all;">
                Or copy this link: %s
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, link, link)

	return subject, textBody, htmlBody
}
