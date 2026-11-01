package web

import "embed"

// ============================================================
// EMBEDDED STATIC ASSETS
//
// Membundel seluruh isi folder web/ (landing page, pricing,
// signup, login, style.css) ke dalam binary saat compile time.
//
// Package ini SENGAJA berdiri sendiri (bukan bagian dari
// cmd/saas-api atau core/) — tujuannya supaya static assets
// tetap jadi unit terpisah yang bisa diganti/dipindah tanpa
// menyentuh composition root atau mesin modular.
//
// "*" merekam semua file & subfolder langsung di dalam web/
// (termasuk static/) secara rekursif, kecuali file yang
// namanya diawali "." atau "_".
// ============================================================

//go:embed *
var Assets embed.FS