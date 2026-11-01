// core/dependency/resolver.go
package dependency

import (
	"fmt"
	"strings"

	"trailseal/sdk"
)

// Resolver adalah implementasi DependencyResolver
type Resolver struct {
	manifests  map[string]sdk.Manifest
	graph      map[string]*sdk.DependencyNode
	visited    map[string]bool
	recStack   map[string]bool
	order      []string
}

// NewResolver membuat dependency resolver baru
func NewResolver() *Resolver {
	return &Resolver{
		manifests: make(map[string]sdk.Manifest),
		graph:     make(map[string]*sdk.DependencyNode),
		visited:   make(map[string]bool),
		recStack:  make(map[string]bool),
		order:     []string{},
	}
}

// BuildGraph membangun dependency graph dari semua manifest
func (r *Resolver) BuildGraph(manifests []sdk.Manifest) error {
	r.manifests = make(map[string]sdk.Manifest)
	r.graph = make(map[string]*sdk.DependencyNode)
	r.visited = make(map[string]bool)
	r.recStack = make(map[string]bool)
	r.order = []string{}

	// Build node untuk setiap manifest
	for _, m := range manifests {
		r.manifests[m.Name] = m
		r.graph[m.Name] = &sdk.DependencyNode{
			Name:         m.Name,
			Version:      m.Version,
			Dependencies: []string{},
			Dependents:   []string{},
		}
	}

	// Build edges dari dependencies
	for _, m := range manifests {
		node := r.graph[m.Name]
		for _, dep := range m.Dependencies {
			// Parse dependency (bisa dalam format "name@version")
			depName := parseDependencyName(dep)

			// Cek apakah dependency exists
			if _, exists := r.graph[depName]; !exists {
				return fmt.Errorf("dependency %s not found for plugin %s", dep, m.Name)
			}

			node.Dependencies = append(node.Dependencies, depName)
			r.graph[depName].Dependents = append(r.graph[depName].Dependents, m.Name)
		}
	}

	return nil
}

// ResolveOrder mengembalikan urutan load yang benar (topological order)
func (r *Resolver) ResolveOrder() ([]string, error) {
	r.visited = make(map[string]bool)
	r.recStack = make(map[string]bool)
	r.order = []string{}

	// DFS untuk topological sort
	for name := range r.graph {
		if !r.visited[name] {
			if err := r.dfs(name); err != nil {
				return nil, err
			}
		}
	}

	// Reverse order (dependencies first)
	for i, j := 0, len(r.order)-1; i < j; i, j = i+1, j-1 {
		r.order[i], r.order[j] = r.order[j], r.order[i]
	}

	return r.order, nil
}

// dfs melakukan depth-first search untuk topological sort
func (r *Resolver) dfs(name string) error {
	r.visited[name] = true
	r.recStack[name] = true

	node := r.graph[name]
	for _, dep := range node.Dependencies {
		if !r.visited[dep] {
			if err := r.dfs(dep); err != nil {
				return err
			}
		} else if r.recStack[dep] {
			// Circular dependency detected
			cycle := r.getCycle(dep)
			return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " → "))
		}
	}

	r.recStack[name] = false
	r.order = append(r.order, name)
	return nil
}

// getCycle mengembalikan circular dependency cycle
func (r *Resolver) getCycle(start string) []string {
	cycle := []string{start}
	current := start

	for {
		node := r.graph[current]
		found := false
		for _, dep := range node.Dependencies {
			if r.recStack[dep] {
				cycle = append(cycle, dep)
				current = dep
				found = true
				break
			}
		}
		if !found || current == start {
			break
		}
	}

	return cycle
}

// GetDependencies mengembalikan semua dependency dari sebuah plugin
func (r *Resolver) GetDependencies(name string) []string {
	if node, exists := r.graph[name]; exists {
		return node.Dependencies
	}
	return []string{}
}

// GetDependents mengembalikan semua plugin yang bergantung pada sebuah plugin
func (r *Resolver) GetDependents(name string) []string {
	if node, exists := r.graph[name]; exists {
		return node.Dependents
	}
	return []string{}
}

// HasCircularDependency mengecek apakah ada circular dependency
func (r *Resolver) HasCircularDependency() bool {
	// Coba resolve, jika error maka ada circular
	_, err := r.ResolveOrder()
	return err != nil
}

// GetCircularDependencies mengembalikan semua circular dependency yang ditemukan
func (r *Resolver) GetCircularDependencies() [][]string {
	// Implementasi sederhana: coba resolve dan tangkap error
	_, err := r.ResolveOrder()
	if err == nil {
		return [][]string{}
	}

	// Parse error untuk mendapatkan cycle
	errMsg := err.Error()
	if strings.Contains(errMsg, "circular dependency detected:") {
		parts := strings.SplitN(errMsg, ": ", 2)
		if len(parts) == 2 {
			cycle := strings.Split(parts[1], " → ")
			return [][]string{cycle}
		}
	}

	return [][]string{}
}

// parseDependencyName memparsing nama dependency dari string "name@version"
func parseDependencyName(dep string) string {
	// Format: "name@version" atau "name"
	parts := strings.Split(dep, "@")
	if len(parts) >= 1 {
		return strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(dep)
}