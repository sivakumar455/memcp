// Package session manages chat session lifecycle for memcp.
package session

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/sivakumar455/memcp/internal/memory"
)

// Manager handles session creation, listing, and switching.
type Manager struct {
	store   *memory.Store
	current *memory.Session
	mu      sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(store *memory.Store) *Manager {
	return &Manager{store: store}
}

// Current returns the current active session, creating a default if none exists.
func (m *Manager) Current() (*memory.Session, error) {
	m.mu.RLock()
	if m.current != nil {
		sess := m.current
		m.mu.RUnlock()
		return sess, nil
	}
	m.mu.RUnlock()

	// Try to auto-create or resume the default session
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check after acquiring write lock
	if m.current != nil {
		return m.current, nil
	}

	// Look for existing "default" session
	sess, err := m.store.GetSessionByName("default")
	if err == nil {
		m.current = sess
		return sess, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("looking up default session: %w", err)
	}

	// Create default session
	sess, err = m.store.CreateSession("default")
	if err != nil {
		return nil, fmt.Errorf("creating default session: %w", err)
	}
	m.current = sess
	return sess, nil
}

// Create creates a new named session and makes it active.
func (m *Manager) Create(name string) (*memory.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.store.CreateSession(name)
	if err != nil {
		return nil, fmt.Errorf("creating session %q: %w", name, err)
	}
	m.current = sess
	return sess, nil
}

// Switch switches to an existing session by name or ID.
func (m *Manager) Switch(nameOrID string) (*memory.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try by name first
	sess, err := m.store.GetSessionByName(nameOrID)
	if err == nil {
		m.current = sess
		return sess, nil
	}

	// Try by ID
	sess, err = m.store.GetSession(nameOrID)
	if err == nil {
		m.current = sess
		return sess, nil
	}

	return nil, fmt.Errorf("session %q not found", nameOrID)
}

// List returns all sessions.
func (m *Manager) List() ([]*memory.Session, error) {
	return m.store.ListSessions()
}

// CurrentID returns the current session ID, or empty string if none.
func (m *Manager) CurrentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current != nil {
		return m.current.ID
	}
	return ""
}

// CurrentName returns the current session name, or empty string if none.
func (m *Manager) CurrentName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current != nil {
		return m.current.Name
	}
	return ""
}
