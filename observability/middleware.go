// observability/middleware.go
package observability

import (
	"fmt"
	"log"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"
)

// ErrorReporting harus dipasang PALING AWAL di chain middleware
// (sebelum CORS/auth/dsb), supaya:
//  1. panic APAPUN di handler manapun di-recover, dilaporkan ke
//     Sentry DENGAN stack trace, lalu direspon sebagai 500 (bukan
//     koneksi mati begitu saja).
//  2. response dengan status >= 500 yang TIDAK panic (mis. handler
//     sengaja return 500 karena DB error) tetap dilaporkan ke Sentry,
//     supaya error rate terlihat di dashboard walau tidak fatal.
//
// onCritical dipanggil untuk kedua kasus di atas — dipakai app.go
// untuk menyalurkan ke alerts.NotifyCritical tanpa membuat package
// observability import package alerts (menghindari import cycle,
// dan supaya observability tetap bisa dipakai berdiri sendiri).
func ErrorReporting(onCritical func(title, message string, fields map[string]string)) fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()

				tags := map[string]string{
					"path":   c.Path(),
					"method": c.Method(),
				}
				CapturePanic(r, stack, tags)

				if onCritical != nil {
					onCritical(
						"Panic recovered",
						fmt.Sprintf("%s %s panicked: %v", c.Method(), c.Path(), r),
						tags,
					)
				}

				log.Printf("ERROR: panic recovered on %s %s: %v\n%s", c.Method(), c.Path(), r, stack)

				err = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal_server_error",
				})
			}
		}()

		err = c.Next()

		status := c.Response().StatusCode()
		if status >= fiber.StatusInternalServerError {
			tags := map[string]string{
				"path":   c.Path(),
				"method": c.Method(),
				"status": fmt.Sprintf("%d", status),
			}

			msg := fmt.Sprintf("%s %s returned %d", c.Method(), c.Path(), status)
			CaptureError(fmt.Errorf("%s", msg), tags)

			// 5xx non-panic dilaporkan ke Sentry saja, TIDAK ke alerts
			// webhook — kalau tidak, satu dependency down (mis. Postgres
			// flaky) bisa membanjiri Slack/Discord dengan alert per
			// request. Alert kritikal dipicu secara terpisah dan
			// sengaja (quota exceeded, chain broken, panic) dari
			// caller yang tahu persis kapan itu benar-benar kritikal.
		}

		return err
	}
}
