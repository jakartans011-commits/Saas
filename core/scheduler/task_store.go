// core/scheduler/task_store.go
package scheduler

import (
	"sync"

	"trailseal/sdk"
)

// TaskStore adalah penyimpanan executable (in-memory)
type TaskStore struct {
	mu          sync.RWMutex
	executables map[string]sdk.Executable
	byPlugin    map[string][]string // pluginName -> []executableName
}

// NewTaskStore membuat task store baru
func NewTaskStore() *TaskStore {
	return &TaskStore{
		executables: make(map[string]sdk.Executable),
		byPlugin:    make(map[string][]string),
	}
}

// Add menambahkan executable ke store
func (s *TaskStore) Add(executable sdk.Executable, pluginName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := executable.Name()
	if _, exists := s.executables[name]; exists {
		return nil // sudah ada
	}

	s.executables[name] = executable
	s.byPlugin[pluginName] = append(s.byPlugin[pluginName], name)

	return nil
}

// Remove menghapus executable dari store
func (s *TaskStore) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.executables[name]; !exists {
		return nil
	}

	// Cari dan hapus dari byPlugin
	for plugin, names := range s.byPlugin {
		for i, n := range names {
			if n == name {
				s.byPlugin[plugin] = append(names[:i], names[i+1:]...)
				if len(s.byPlugin[plugin]) == 0 {
					delete(s.byPlugin, plugin)
				}
				break
			}
		}
	}

	delete(s.executables, name)
	return nil
}

// Get mengembalikan executable dari store
func (s *TaskStore) Get(name string) (sdk.Executable, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.executables[name]
	return exec, ok
}

// List mengembalikan semua executable
func (s *TaskStore) List() []sdk.Executable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]sdk.Executable, 0, len(s.executables))
	for _, exec := range s.executables {
		result = append(result, exec)
	}
	return result
}

// ListByPlugin mengembalikan executable untuk plugin tertentu
func (s *TaskStore) ListByPlugin(pluginName string) []sdk.Executable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names, ok := s.byPlugin[pluginName]
	if !ok {
		return []sdk.Executable{}
	}

	result := make([]sdk.Executable, 0, len(names))
	for _, name := range names {
		if exec, exists := s.executables[name]; exists {
			result = append(result, exec)
		}
	}
	return result
}

// Count mengembalikan jumlah executable
func (s *TaskStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.executables)
}

// CountByPlugin mengembalikan jumlah executable per plugin
func (s *TaskStore) CountByPlugin(pluginName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names, ok := s.byPlugin[pluginName]
	if !ok {
		return 0
	}
	return len(names)
}
