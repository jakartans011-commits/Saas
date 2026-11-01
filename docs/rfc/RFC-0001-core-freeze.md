# RFC-0001: Core Freeze

## Status: ACCEPTED

## Date: 2026-08-06

## Author: ShadowHarvest Architecture Team

## Summary

Core ShadowHarvest mencapai status FROZEN. Tidak ada perubahan yang boleh dilakukan pada Core kecuali melalui RFC.

## Motivation

Core terlalu sering berubah untuk setiap module baru. Ini menyebabkan:

- Build tidak stabil
- Module saling bergantung
- Maintenance mahal
- Developer kesulitan onboarding

## Solution

Freeze Core. Semua fitur baru hanya di module.

## Impact

- ✅ Core stabil
- ✅ Module independen
- ✅ Build stabil
- ✅ Developer mudah onboarding

## Implementation

1. Buat SDK Contract Layer ✅
2. Buat Adapter Layer ✅
3. Buat Stable Runtime ✅
4. Buat Registry ✅
5. Buat Plugin Manager ✅
6. Buat Lifecycle ✅
7. Buat Migration Layer ✅
8. Migrasi module satu per satu 🔄
9. Core Freeze ✅
10. Hapus Core lama ⬜

## Checklist

- [x] SDK Contract Layer
- [x] Adapter Layer
- [x] Stable Runtime
- [x] Registry
- [x] Plugin Manager
- [x] Lifecycle
- [x] Migration Layer
- [x] Module Migration (Recon)
- [x] Core Freeze Announcement
- [ ] All modules migrated (ongoing)
- [ ] Old Core removed (future)

## Approved By

- Architecture Team ✅
- Engineering Team ✅
