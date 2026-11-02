// Package migrations menanam berkas .sql di direktori ini ke dalam binary.
//
// go:embed hanya bisa menanam dari direktori paketnya sendiri, dan berkas
// migrasi hidup di akar repo — bukan di bawah db/. Karena itu paket kecil
// ini ada di sini, menanam berkasnya, lalu dipakai oleh db.RunMigrations.
// Berkas .sql-nya tidak berpindah, dan perkakas lain yang membacanya dari
// disk tetap bekerja seperti sebelumnya.
package migrations

import "embed"

// FS berisi seluruh berkas migrasi, tertanam saat build.
//
//go:embed *.sql
var FS embed.FS
