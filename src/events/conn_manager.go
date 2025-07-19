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

	// Get all related users (friends and relationships)
	relatedUsers, err := userRepo.GetRelatedUserIDs(userID)
	if err != nil {
		return fmt.Errorf("failed to get related users: %v", err)
	}

	// Create a map to track unique user IDs
	userIDsMap := make(map[string]bool)
	for _, id := range relatedUsers {
		userIDsMap[id] = true
	}

	// Get all rooms the user is in
	rooms, err := userRepo.GetUserRooms(userID)
	if err != nil {
		log.Printf("Failed to get user rooms: %v", err)
		// Continue with the users we have, don't fail completely
	} else {
		// Add all recipients from group PMs
		for _, room := range rooms {
			// Type 1 is Group PM
			if room.Type == 1 {
				for _, recipientID := range room.Recipients {
					// Don't add the user themselves
					if recipientID != userID {
						userIDsMap[recipientID] = true
					}
				}
			}
		}
	}

	// Get all spaces the user is in and add space members
	spaces, err := userRepo.GetUserSpaces(userID)
	if err != nil {
		log.Printf("Failed to get user spaces: %v", err)
		// Continue with the users we have, don't fail completely
	} else {
		// Add all space members
		for _, space := range spaces {
			members, err := userRepo.GetSpaceMembers(space.ID)
			if err != nil {
				log.Printf("Failed to get members for space %s: %v", space.ID, err)
				continue
			}
			for _, memberUserID := range members {
				// Don't add the user themselves
				if memberUserID != userID {
					userIDsMap[memberUserID] = true
				}
			}
		}
	}

	// Convert map back to slice
	relatedUsers = make([]string, 0, len(userIDsMap))
	for id := range userIDsMap {
		relatedUsers = append(relatedUsers, id)
	}

   log.Printf("Broadcasting presence update to %d users (including group members and space members)", len(relatedUsers))

	// Create presence update payload
	payload := PresenceUpdatePayload{
		BasePayload: BasePayload{
			Type: PayloadTypePresenceUpdate,
		},
		UserID:       userID,
		Status:       status,
		CustomStatus: customStatus,
	}

// Create a list of all users to broadcast to (including the current user)
	allUsers := append([]string{userID}, relatedUsers...)

	// Use goroutines for concurrent broadcasting
	var wg sync.WaitGroup
	for _, targetUserID := range allUsers {
		if handlers, exists := m.connections[targetUserID]; exists {
			for handler := range handlers {
				wg.Add(1)
				go func(h *WebSocketHandler, uid string) {
					defer wg.Done()
					if err := h.sendResponse(payload); err != nil {
						log.Printf("Failed to send presence update to user %s: %v", uid, err)
					} else {
						log.Printf("Successfully sent presence update to user %s", uid)
					}
				}(handler, targetUserID)
			}
		}
	}

	// Wait for all broadcasts to complete
	wg.Wait()

	return nil
}

func (m *ConnectionManager) BroadcastSpaceCreate(spaceData map[string]interface{}, userRepo *repository.UserRepository) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get space ID and owner ID from space data
	spaceID, ok := spaceData["id"].(string)
	if !ok {
		return fmt.Errorf("space data has no id")
	}

	ownerID, ok := spaceData["owner_id"].(string)
	if !ok {
		return fmt.Errorf("space data has no owner_id")
	}

	log.Printf("Broadcasting space create event: Space=%s, Owner=%s", spaceID, ownerID)

	// Create space create payload
	payload := SpaceCreatePayload{
		BasePayload: BasePayload{
			Type: PayloadTypeSpaceCreate,
		},
		Space: spaceData,
	}

	// Send to the space owner's connections
	if handlers, exists := m.connections[ownerID]; exists {
		for handler := range handlers {
			if err := handler.sendResponse(payload); err != nil {
				log.Printf("Failed to send space create to owner %s: %v", ownerID, err)
			}
		}
	}

	return nil
}
