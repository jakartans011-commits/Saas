// core/registry.go
package core

import (
	"sync"

	"trailseal/sdk"
)

var (
	regMu     sync.RWMutex
	plugins   = make(map[string]func(sdk.Container) sdk.Plugin)
	manifests = make(map[string]sdk.Manifest)
)

// RegisterPlugin mendaftarkan plugin dengan factory yang menerima container
func RegisterPlugin(name string, factory func(sdk.Container) sdk.Plugin) {
	regMu.Lock()
	defer regMu.Unlock()
	plugins[name] = factory
}

// RegisterManifest mendaftarkan manifest plugin (TANPA membuat instance!)
func RegisterManifest(name string, manifest sdk.Manifest) {
	regMu.Lock()
	defer regMu.Unlock()
	manifests[name] = manifest
}

// GetPlugin mengembalikan factory plugin
func GetPlugin(name string) (func(sdk.Container) sdk.Plugin, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := plugins[name]
	return f, ok
}

// GetManifest mengembalikan manifest plugin
func GetManifest(name string) (sdk.Manifest, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	m, ok := manifests[name]
	return m, ok
}

// ListPlugins mengembalikan semua plugin yang terdaftar
func ListPlugins() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	return names
}

// ListManifests mengembalikan semua manifest yang terdaftar
func ListManifests() []sdk.Manifest {
	regMu.RLock()
	defer regMu.RUnlock()
	result := make([]sdk.Manifest, 0, len(manifests))
	for _, m := range manifests {
		result = append(result, m)
	}
	return result
}