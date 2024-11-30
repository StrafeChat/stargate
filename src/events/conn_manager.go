package events

import (
	"sync"
)

type ConnectionManager struct {
	connections map[string]map[*WebSocketHandler]bool
	mu          sync.RWMutex
}

var Manager = &ConnectionManager{
	connections: make(map[string]map[*WebSocketHandler]bool),
}

func (m *ConnectionManager) AddConnection(userID string, handler *WebSocketHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connections[userID] == nil {
		m.connections[userID] = make(map[*WebSocketHandler]bool)
	}
	m.connections[userID][handler] = true
}

func (m *ConnectionManager) RemoveConnection(userID string, handler *WebSocketHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if handlers, exists := m.connections[userID]; exists {
		delete(handlers, handler)
		if len(handlers) == 0 {
			delete(m.connections, userID)
		}
	}
}

func (m *ConnectionManager) HasOtherConnections(userID string, handler *WebSocketHandler) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if handlers, exists := m.connections[userID]; exists {
		count := 0
		for h := range handlers {
			if h != handler {
				count++
			}
		}
		return count > 0
	}
	return false
}
