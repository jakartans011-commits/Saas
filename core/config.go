// core/config.go
package core

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"
)

// ============================================================================
// BEACON CONFIG - PINDAHKAN KE SINI
// ============================================================================

// BeaconConfig - Konfigurasi lengkap untuk beacon client
type BeaconConfig struct {
	// Endpoints
	Endpoints []string `json:"endpoints"`

	// Authentication
	AuthToken string `json:"auth_token"`
	SecretKey string `json:"secret_key"`
	ClientID  string `json:"client_id"`

	// Network
	Timeout    time.Duration `json:"timeout"`
	Method     string        `json:"method"`
	MaxRetries int           `json:"max_retries"`
	RetryDelay time.Duration `json:"retry_delay"`

	// Adaptive Sleep
	EnableAdaptiveSleep bool `json:"enable_adaptive_sleep"`
	PeakHourStart       int  `json:"peak_hour_start"`
	PeakHourEnd         int  `json:"peak_hour_end"`
	PeakHourInterval    int  `json:"peak_hour_interval"`
	OffPeakInterval     int  `json:"off_peak_interval"`

	// Domain Fronting
	EnableDomainFronting bool   `json:"enable_domain_fronting"`
	FrontDomain          string `json:"front_domain"`
	BackendHost          string `json:"backend_host"`

	// Interval
	Interval time.Duration `json:"interval"`

	// Jitter
	EnableJitter bool `json:"enable_jitter"`
	JitterMin    int  `json:"jitter_min"`
	JitterMax    int  `json:"jitter_max"`

	// Circuit Breaker
	CircuitBreakerThreshold int           `json:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `json:"circuit_breaker_timeout"`

	// Queue
	QueueSize int `json:"queue_size"`

	// Encryption
	EnableEncryption bool   `json:"enable_encryption"`
	EncryptionKey    string `json:"encryption_key"`

	// Proxy
	ProxyURL string `json:"proxy_url"`

	// Transport
	InsecureSkipVerify bool `json:"insecure_skip_verify"`

	// Headers
	CustomHeaders map[string]string `json:"custom_headers"`

	// Logging
	LogLevel string `json:"log_level"`
}

// DefaultBeaconConfig - konfigurasi default
func DefaultBeaconConfig() *BeaconConfig {
	return &BeaconConfig{
		Endpoints: []string{
			"https://api.github.com/repos/kuda1414/c2-beacon/issues",
		},
		Timeout:      30 * time.Second,
		Method:       "POST",
		MaxRetries:   3,
		RetryDelay:   5 * time.Second,
		Interval:     30 * time.Second,
		EnableJitter: true,
		JitterMin:    45,
		JitterMax:    75,

		EnableAdaptiveSleep: true,
		PeakHourStart:       8,
		PeakHourEnd:         18,
		PeakHourInterval:    30,
		OffPeakInterval:     120,

		EnableDomainFronting: false,
		FrontDomain:          "cdn.cloudflare.com",
		BackendHost:          "api.github.com",

		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   60 * time.Second,
		QueueSize:               100,
		EnableEncryption:        false,
		EncryptionKey:           "",
		InsecureSkipVerify:      false,
		CustomHeaders: map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		},
		LogLevel: "info",
	}
}

// ============================================================================
// AGENT CONFIG
// ============================================================================

// Config - Agent configuration
type Config struct {
	AgentID             string                 `json:"agent_id"`
	AgentName           string                 `json:"agent_name"`
	AgentTags           []string               `json:"agent_tags"`
	HeartbeatInterval   time.Duration          `json:"heartbeat_interval"`
	CycleInterval       time.Duration          `json:"cycle_interval"`
	HealthCheckInterval time.Duration          `json:"health_check_interval"`
	EnableModules       bool                   `json:"enable_modules"`
	EnableBeacon        bool                   `json:"enable_beacon"`
	EnableLogging       bool                   `json:"enable_logging"`
	Beacon              *BeaconConfig          `json:"beacon"`
	ModuleConfig        map[string]interface{} `json:"module_config"`
	Logging             *LogConfig             `json:"logging"`
	StoragePath         string                 `json:"storage_path"`
	LogPath             string                 `json:"log_path"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		AgentID:             "agent-default",
		AgentName:           "TrailSeal-Agent",
		AgentTags:           []string{},
		HeartbeatInterval:   30 * time.Second,
		CycleInterval:       10 * time.Second,
		HealthCheckInterval: 60 * time.Second,
		EnableModules:       true,
		EnableBeacon:        true,
		EnableLogging:       true,
		Beacon:              DefaultBeaconConfig(),
		ModuleConfig:        make(map[string]interface{}),
		Logging:             nil,
		StoragePath:         "D:/TrailSeal/data/agent.db",
		LogPath:             "D:/TrailSeal/logs/agent.log",
	}
}

// GetModuleConfig returns module configuration
func (c *Config) GetModuleConfig() map[string]interface{} {
	return c.ModuleConfig
}

// ============================================================================
// LOAD CONFIG FROM FILE
// ============================================================================

// LoadConfigFromFile loads configuration from JSON file
func LoadConfigFromFile(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// ============================================================================
// BEACON CONFIG LOADER
// ============================================================================

// LoadBeaconConfigFromFile - loads beacon config from file
func LoadBeaconConfigFromFile(path string) (*BeaconConfig, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var config BeaconConfig

	if beaconRaw, ok := raw["beacon"]; ok {
		beaconData, err := json.Marshal(beaconRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal beacon field: %w", err)
		}
		if err := json.Unmarshal(beaconData, &config); err != nil {
			return nil, fmt.Errorf("failed to parse beacon config: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	if len(config.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint is required")
	}
	if config.AuthToken == "" {
		return nil, fmt.Errorf("auth_token is required")
	}
	if config.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}

	return &config, nil
}
