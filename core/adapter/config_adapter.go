// core/adapter/config_adapter.go
package adapter

import (
	"encoding/json"
	"os"
	"time"

	"trailseal/sdk"
)

// ConfigAdapter adalah adapter untuk konfigurasi
type ConfigAdapter struct {
	data map[string]interface{}
}

// NewConfigAdapter membuat adapter baru dari file
func NewConfigAdapter(configPath string) sdk.Config {
	data := make(map[string]interface{})

	if configPath != "" {
		fileData, err := os.ReadFile(configPath)
		if err == nil {
			json.Unmarshal(fileData, &data)
		}
	}

	return &ConfigAdapter{
		data: data,
	}
}

func (c *ConfigAdapter) Get(key string) interface{} {
	return c.data[key]
}

func (c *ConfigAdapter) GetString(key string) string {
	if v, ok := c.data[key].(string); ok {
		return v
	}
	return ""
}

func (c *ConfigAdapter) GetInt(key string) int {
	if v, ok := c.data[key].(float64); ok {
		return int(v)
	}
	if v, ok := c.data[key].(int); ok {
		return v
	}
	return 0
}

func (c *ConfigAdapter) GetBool(key string) bool {
	if v, ok := c.data[key].(bool); ok {
		return v
	}
	return false
}

func (c *ConfigAdapter) GetDuration(key string) time.Duration {
	if v, ok := c.data[key].(float64); ok {
		return time.Duration(v) * time.Second
	}
	if v, ok := c.data[key].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 0
}

func (c *ConfigAdapter) GetStringSlice(key string) []string {
	if v, ok := c.data[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return []string{}
}

func (c *ConfigAdapter) Set(key string, value interface{}) {
	c.data[key] = value
}

func (c *ConfigAdapter) Unset(key string) {
	delete(c.data, key)
}

func (c *ConfigAdapter) All() map[string]interface{} {
	return c.data
}