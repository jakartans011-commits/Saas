// pkg/utils/utils.go
package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"os"
	"runtime"
	"time"
)

// GenerateRandomKey - generate random key
func GenerateRandomKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// SHA256Hash - hash data dengan SHA256
func SHA256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// GetLocalIP - dapatkan IP lokal
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "unknown"
}

// GetHostname - dapatkan hostname
func GetHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

// GetOS - dapatkan OS
func GetOS() string {
	return runtime.GOOS
}

// RandomDelay - random delay antara min dan max
func RandomDelay(min, max int) time.Duration {
	if min > max {
		min, max = max, min
	}
	if min == 0 && max == 0 {
		return 0
	}
	seconds := min
	if max > min {
		seconds = min + time.Now().Nanosecond()%(max-min)
	}
	return time.Duration(seconds) * time.Second
}
