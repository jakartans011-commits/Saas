package audit

import "strings"

// ActionLoginFailed dicatat MANUAL oleh handler login. AuditLogger tidak
// menangkap login karena middleware itu melewati semua path /api/auth/*.
const ActionLoginFailed = "login_failed"

// IsSecurityEvent mengenali action yang termasuk kategori security V1.
//
// Format action dari middleware.AuditLogger: METHOD + " " + path, contoh
// "POST /api/tenants/abc/users". Kategori yang dikenali:
//   - login_failed                                            (login gagal)
//   - POST /api/tenants/:id/users                             (user dibuat)
//   - POST /api/tenants/:id/projects/:pid/keys                (API key dibuat)
//   - POST /api/tenants/:id/projects/:pid/keys/:kid/revoke    (API key dicabut)
//
// Outcome sengaja tidak dilihat: percobaan yang ditolak (denied/failure)
// juga relevan untuk keamanan.
func IsSecurityEvent(action string) bool {
	if action == ActionLoginFailed {
		return true
	}

	method, path, ok := strings.Cut(action, " ")
	if !ok || method != "POST" {
		return false
	}

	seg := strings.Split(strings.Trim(strings.ToLower(path), "/"), "/")
	if len(seg) < 4 || seg[0] != "api" || seg[1] != "tenants" {
		return false
	}

	switch len(seg) {
	case 4: // api/tenants/:id/users
		return seg[3] == "users"
	case 6: // api/tenants/:id/projects/:pid/keys
		return seg[3] == "projects" && seg[5] == "keys"
	case 8: // api/tenants/:id/projects/:pid/keys/:kid/revoke
		return seg[3] == "projects" && seg[5] == "keys" && seg[7] == "revoke"
	}
	return false
}