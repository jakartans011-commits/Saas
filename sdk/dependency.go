// sdk/dependency.go
package sdk

// DependencyResolver adalah interface untuk dependency resolution
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type DependencyResolver interface {
	// BuildGraph membangun dependency graph dari semua manifest
	BuildGraph(manifests []Manifest) error

	// ResolveOrder mengembalikan urutan load yang benar (topological order)
	ResolveOrder() ([]string, error)

	// GetDependencies mengembalikan semua dependency dari sebuah plugin
	GetDependencies(name string) []string

	// GetDependents mengembalikan semua plugin yang bergantung pada sebuah plugin
	GetDependents(name string) []string

	// HasCircularDependency mengecek apakah ada circular dependency
	HasCircularDependency() bool

	// GetCircularDependencies mengembalikan semua circular dependency yang ditemukan
	GetCircularDependencies() [][]string
}

// DependencyNode adalah node dalam dependency graph
type DependencyNode struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Dependencies []string `json:"dependencies"`
	Dependents   []string `json:"dependents"`
}