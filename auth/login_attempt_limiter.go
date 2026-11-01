package auth

import (
	"strings"
	"sync"
	"time"
)

const (
	LoginAttemptWindow = time.Minute
	LoginMaxFailures   = 12
)

type LoginAttemptLimiter struct {
	mu      sync.Mutex
	records map[string]*loginAttemptRecord
}

type loginAttemptRecord struct {
	failures  int
	windowEnd time.Time
}

func NewLoginAttemptLimiter() *LoginAttemptLimiter {
	return &LoginAttemptLimiter{
		records: make(map[string]*loginAttemptRecord),
	}
}

// Allow menentukan apakah percobaan login masih diperbolehkan.
//
// Key sengaja menggunakan IP + email:
//   - melindungi akun dari brute-force password
//   - tidak langsung mengunci akun secara permanen
//   - window otomatis berakhir setelah satu menit
func (l *LoginAttemptLimiter) Allow(ip, email string) bool {
	key := loginAttemptKey(ip, email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[key]
	if !ok {
		return true
	}

	if now.After(rec.windowEnd) {
		delete(l.records, key)
		return true
	}

	return rec.failures < LoginMaxFailures
}

// RecordFailure mencatat kegagalan login.
func (l *LoginAttemptLimiter) RecordFailure(ip, email string) {
	key := loginAttemptKey(ip, email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[key]

	if !ok || now.After(rec.windowEnd) {
		l.records[key] = &loginAttemptRecord{
			failures:  1,
			windowEnd: now.Add(LoginAttemptWindow),
		}
		return
	}

	rec.failures++
}

// Reset menghapus kegagalan setelah login berhasil.
func (l *LoginAttemptLimiter) Reset(ip, email string) {
	key := loginAttemptKey(ip, email)

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.records, key)
}

func loginAttemptKey(ip, email string) string {
	return strings.TrimSpace(ip) + "\x00" +
		strings.ToLower(strings.TrimSpace(email))
}
