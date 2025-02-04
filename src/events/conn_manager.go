package events

import (
	"fmt"
	"log"
	"sync"

	"github.com/StrafeChat/stargate/src/repository"
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

func (m *ConnectionManager) BroadcastPresenceUpdate(userID string, status string, customStatus string, userRepo *repository.UserRepository) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get all related users
	relatedUsers, err := userRepo.GetRelatedUserIDs(userID)
	if err != nil {
		return fmt.Errorf("failed to get related users: %v", err)
	}

   log.Printf("Broadcasting presence update to %d related users", len(relatedUsers))

	// Create presence update payload
	payload := PresenceUpdatePayload{
		BasePayload: BasePayload{
			Type: PayloadTypePresenceUpdate,
		},
		UserID:       userID,
		Status:       status,
		CustomStatus: customStatus,
	}

// First send to the current user's connections
if handlers, exists := m.connections[userID]; exists {
    for handler := range handlers {
        if err := handler.sendResponse(payload); err != nil {
            log.Printf("Failed to send presence update to user %s: %v", userID, err)
        }
    }
}

// Then send to related users' connections
for _, relatedUserID := range relatedUsers {
    if handlers, exists := m.connections[relatedUserID]; exists {
        for handler := range handlers {
            fmt.Printf("Broadcasting presence update to user %s\n", relatedUserID)
            if err := handler.sendResponse(payload); err != nil {
                log.Printf("Failed to send presence update to user %s: %v", relatedUserID, err)
            }
        }
    }
}

	return nil
}
