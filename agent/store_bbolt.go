package agent

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BOLTDB STORE — Agent dan Tool
// ============================================================

var (
	agentsBucket = []byte("agents")
	toolsBucket  = []byte("tools")
)

type BoltAgentStore struct {
	db *bolt.DB
}

func NewBoltAgentStore(db *bolt.DB) (*BoltAgentStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(agentsBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(toolsBucket); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init agent buckets: %w", err)
	}
	return &BoltAgentStore{db: db}, nil
}

// ============================================================
// AGENT CRUD
// ============================================================

func (s *BoltAgentStore) SaveAgent(agent *Agent) error {
	if agent.TenantID == "" {
		return fmt.Errorf("agent: tenant_id is required")
	}
	if agent.ID == "" {
		return fmt.Errorf("agent: id is required")
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now()
	}
	agent.UpdatedAt = time.Now()

	data, err := json.Marshal(agent)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		key := []byte(agent.TenantID + ":" + agent.ID)
		return b.Put(key, data)
	})
}

func (s *BoltAgentStore) GetAgent(tenantID, agentID string) (*Agent, error) {
	agent, err := s.GetAgentIncludingDeleted(tenantID, agentID)
	if err != nil {
		return nil, err
	}
	if agent.DeletedAt != nil {
		return nil, fmt.Errorf("agent not found")
	}
	return agent, nil
}

// GetAgentIncludingDeleted returns agent even if soft-deleted
func (s *BoltAgentStore) GetAgentIncludingDeleted(tenantID, agentID string) (*Agent, error) {
	var agent Agent
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		key := []byte(tenantID + ":" + agentID)
		data := b.Get(key)
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &agent)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("agent not found")
	}
	return &agent, nil
}

func (s *BoltAgentStore) ListAgents(tenantID string) ([]*Agent, error) {
	agents := make([]*Agent, 0)
	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
			var agent Agent
			if err := json.Unmarshal(v, &agent); err != nil {
				continue
			}
			// Exclude soft-deleted
			if agent.DeletedAt == nil {
				agents = append(agents, &agent)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return agents, nil
}

// ListDeletedAgents returns only soft-deleted agents for a tenant
func (s *BoltAgentStore) ListDeletedAgents(tenantID string) ([]*Agent, error) {
	agents := make([]*Agent, 0)
	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
			var agent Agent
			if err := json.Unmarshal(v, &agent); err != nil {
				continue
			}
			if agent.DeletedAt != nil {
				agents = append(agents, &agent)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *BoltAgentStore) DeleteAgent(tenantID, agentID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		key := []byte(tenantID + ":" + agentID)
		return b.Delete(key)
	})
}

// DeleteAllAgentsForTenant hard-deletes all agents for a tenant
func (s *BoltAgentStore) DeleteAllAgentsForTenant(tenantID string) error {
	prefix := []byte(tenantID + ":")

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		c := b.Cursor()

		var keysToDelete [][]byte
		for k, _ := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, _ = c.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keysToDelete = append(keysToDelete, keyCopy)
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltAgentStore) ListAllAgents() ([]*Agent, error) {
	agents := make([]*Agent, 0)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(agentsBucket)
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var agent Agent
			if err := json.Unmarshal(v, &agent); err != nil {
				continue
			}
			// Exclude soft-deleted for admin list
			if agent.DeletedAt == nil {
				agents = append(agents, &agent)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return agents, nil
}

// ============================================================
// TOOL CRUD
// ============================================================

func (s *BoltAgentStore) SaveTool(tool *Tool) error {
	if tool.TenantID == "" {
		return fmt.Errorf("tool: tenant_id is required")
	}
	if tool.ID == "" {
		return fmt.Errorf("tool: id is required")
	}
	if tool.CreatedAt.IsZero() {
		tool.CreatedAt = time.Now()
	}
	tool.UpdatedAt = time.Now()

	data, err := json.Marshal(tool)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		key := []byte(tool.TenantID + ":" + tool.ID)
		return b.Put(key, data)
	})
}

func (s *BoltAgentStore) GetTool(tenantID, toolID string) (*Tool, error) {
	tool, err := s.GetToolIncludingDeleted(tenantID, toolID)
	if err != nil {
		return nil, err
	}
	if tool.DeletedAt != nil {
		return nil, fmt.Errorf("tool not found")
	}
	return tool, nil
}

// GetToolIncludingDeleted returns tool even if soft-deleted
func (s *BoltAgentStore) GetToolIncludingDeleted(tenantID, toolID string) (*Tool, error) {
	var tool Tool
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		key := []byte(tenantID + ":" + toolID)
		data := b.Get(key)
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &tool)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("tool not found")
	}
	return &tool, nil
}

func (s *BoltAgentStore) ListTools(tenantID string) ([]*Tool, error) {
	tools := make([]*Tool, 0)
	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
			var tool Tool
			if err := json.Unmarshal(v, &tool); err != nil {
				continue
			}
			if tool.DeletedAt == nil {
				tools = append(tools, &tool)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// ListDeletedTools returns only soft-deleted tools for a tenant
func (s *BoltAgentStore) ListDeletedTools(tenantID string) ([]*Tool, error) {
	tools := make([]*Tool, 0)
	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
			var tool Tool
			if err := json.Unmarshal(v, &tool); err != nil {
				continue
			}
			if tool.DeletedAt != nil {
				tools = append(tools, &tool)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func (s *BoltAgentStore) DeleteTool(tenantID, toolID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		key := []byte(tenantID + ":" + toolID)
		return b.Delete(key)
	})
}

// DeleteAllToolsForTenant hard-deletes all tools for a tenant
func (s *BoltAgentStore) DeleteAllToolsForTenant(tenantID string) error {
	prefix := []byte(tenantID + ":")

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		c := b.Cursor()

		var keysToDelete [][]byte
		for k, _ := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, _ = c.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keysToDelete = append(keysToDelete, keyCopy)
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltAgentStore) ListAllTools() ([]*Tool, error) {
	tools := make([]*Tool, 0)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(toolsBucket)
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var tool Tool
			if err := json.Unmarshal(v, &tool); err != nil {
				continue
			}
			if tool.DeletedAt == nil {
				tools = append(tools, &tool)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}
