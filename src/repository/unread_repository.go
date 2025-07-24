package repository

import (
	"fmt"
	"log"

	"github.com/gocql/gocql"
)

type UnreadMessage struct {
	UserID    string `json:"user_id"`
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
}

type MentionUnreadMessage struct {
	UserID    string `json:"user_id"`
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
}

type UnreadRepository struct {
	session *gocql.Session
}

func NewUnreadRepository(session *gocql.Session) *UnreadRepository {
	return &UnreadRepository{
		session: session,
	}
}

// GetUnreadMessagesForUser retrieves all unread messages for a user across all rooms
func (r *UnreadRepository) GetUnreadMessagesForUser(userID string) (map[string][]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	// Initialize map to store room_id -> []message_id
	unreadMessages := make(map[string][]string)

	// First, get all rooms for this user
	roomIDs, err := r.getUserRooms(userID)
	if err != nil {
		log.Printf("Error fetching rooms for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to fetch rooms: %v", err)
	}

	log.Printf("Found %d rooms for user %s", len(roomIDs), userID)

	// For each room, query unread messages using both user_id and room_id (partition key)
	totalCount := 0
	for _, roomID := range roomIDs {
		// Query to get unread messages for the user in this specific room
		log.Printf("Querying unread messages for user %s in room %s", userID, roomID)
		iter := r.session.Query(
			"SELECT message_id FROM message_unreads WHERE user_id = ? AND room_id = ?",
			userID, roomID,
		).Consistency(gocql.One).Iter()

		var messageID string
		roomCount := 0
		for iter.Scan(&messageID) {
			log.Printf("Found unread message: room_id=%s, message_id=%s", roomID, messageID)
			unreadMessages[roomID] = append(unreadMessages[roomID], messageID)
			roomCount++
		}

		if roomCount > 0 {
			log.Printf("Found %d unread messages for user %s in room %s", roomCount, userID, roomID)
		}

		if err := iter.Close(); err != nil {
			log.Printf("Error fetching unread messages for user %s in room %s: %v", userID, roomID, err)
			// Continue with other rooms instead of failing completely
			continue
		}

		totalCount += roomCount
	}

	log.Printf("Found %d total unread messages for user %s across %d rooms", totalCount, userID, len(unreadMessages))

	// If no unread messages found, try a direct check to see if the table has any data
	if len(unreadMessages) == 0 {
		var count int
		if err := r.session.Query("SELECT COUNT(*) FROM message_unreads").Scan(&count); err != nil {
			log.Printf("Error checking message_unreads table: %v", err)
		} else {
			log.Printf("Total records in message_unreads table: %d", count)
		}
	}

	return unreadMessages, nil
}

// getUserRooms retrieves all room IDs for a specific user
func (r *UnreadRepository) getUserRooms(userID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	// Get all room IDs for this user from room_recipients_by_user
	roomIDs := make([]string, 0)
	roomIDsQuery := "SELECT room_id FROM room_recipients_by_user WHERE user_id = ?"
	roomIDsIter := r.session.Query(roomIDsQuery, userID).Iter()

	var roomID string
	for roomIDsIter.Scan(&roomID) {
		roomIDs = append(roomIDs, roomID)
	}

	if err := roomIDsIter.Close(); err != nil {
		log.Printf("Error closing room IDs iterator: %v", err)
		return nil, err
	}

	log.Printf("Found %d room IDs for user %s", len(roomIDs), userID)
	return roomIDs, nil
}

// GetMentionUnreadMessagesForUser retrieves all mention unread messages for a user across all rooms
func (r *UnreadRepository) GetMentionUnreadMessagesForUser(userID string) (map[string][]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	// Initialize map to store room_id -> []message_id
	mentionUnreadMessages := make(map[string][]string)

	// First, get all rooms for this user
	roomIDs, err := r.getUserRooms(userID)
	if err != nil {
		log.Printf("Error fetching rooms for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to fetch rooms: %v", err)
	}

	log.Printf("Found %d rooms for user %s (mention unreads)", len(roomIDs), userID)

	// For each room, query mention unread messages using both user_id and room_id (partition key)
	totalCount := 0
	for _, roomID := range roomIDs {
		// Query to get mention unread messages for the user in this specific room
		log.Printf("Querying mention unread messages for user %s in room %s", userID, roomID)
		iter := r.session.Query(
			"SELECT message_id FROM message_mention_unreads WHERE user_id = ? AND room_id = ?",
			userID, roomID,
		).Consistency(gocql.One).Iter()

		var messageID string
		roomCount := 0
		for iter.Scan(&messageID) {
			log.Printf("Found mention unread message: room_id=%s, message_id=%s", roomID, messageID)
			mentionUnreadMessages[roomID] = append(mentionUnreadMessages[roomID], messageID)
			roomCount++
		}

		if roomCount > 0 {
			log.Printf("Found %d mention unread messages for user %s in room %s", roomCount, userID, roomID)
		}

		if err := iter.Close(); err != nil {
			log.Printf("Error fetching mention unread messages for user %s in room %s: %v", userID, roomID, err)
			// Continue with other rooms instead of failing completely
			continue
		}

		totalCount += roomCount
	}

	log.Printf("Found %d total mention unread messages for user %s across %d rooms", totalCount, userID, len(mentionUnreadMessages))

	return mentionUnreadMessages, nil
}
