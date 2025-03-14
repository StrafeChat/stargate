package repository

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gocql/gocql"
)

var (
	ErrDatabaseNotInitialized = errors.New("database connection not initialized")
	ErrUserNotFound           = errors.New("user not found")
	ErrTokenNotFound          = errors.New("token not found")
	ErrTokenExpired           = errors.New("token expired")
)

type Presence struct {
	Online       bool   `cql:"online"`
	Status       string `cql:"status"`
	CustomStatus string `cql:"custom_status"`
}

type UserPresence struct {
	Status       string `json:"status"`
	CustomStatus string `json:"custom_status"`
}

type UserDetails struct {
	ID            string       `json:"id"`
	Username      string       `json:"username"`
	Discriminator int          `json:"discriminator"`
	DisplayName   string       `json:"display_name"`
	Avatar        string       `json:"avatar"`
	Banner        string       `json:"banner"`
	Bot           bool         `json:"bot"`
	System        bool         `json:"system"`
	Flags         int          `json:"flags"`
	Presence      UserPresence `json:"presence"`
}

type UserRepository struct {
	session *gocql.Session
}

func NewUserRepository(session *gocql.Session) *UserRepository {
	return &UserRepository{
		session: session,
	}
}

func (r *UserRepository) ValidateSessionToken(token string) (string, error) {
	if r.session == nil {
		return "", ErrDatabaseNotInitialized
	}

	var userID string
	var expiresAt int64
	query := r.session.Query("SELECT user_id, expires_at FROM sessions WHERE session_token = ?", token)

	if err := query.Scan(&userID, &expiresAt); err != nil {
		if err == gocql.ErrNotFound {
			log.Printf("Token not found: %s", token)
			return "", ErrTokenNotFound
		}
		log.Printf("Error validating token: %v", err)
		return "", err
	}

	if expiresAt < time.Now().Unix() {
		log.Printf("Token expired: %s", token)
		return "", ErrTokenExpired
	}

	log.Printf("Token valid for user: %s", userID)
	return userID, nil
}

func (r *UserRepository) GetUserDetails(userID string) (UserDetails, error) {
	if r.session == nil {
		return UserDetails{}, ErrDatabaseNotInitialized
	}

	var details UserDetails
	var presence Presence
	var displayName string
	var avatar string
	var banner string
	var bot bool
	var system bool
	var flags int
	if err := r.session.Query("SELECT id, username, discriminator, display_name, avatar, banner, bot, system, flags, presence FROM users WHERE id = ?",
		userID).Scan(&details.ID, &details.Username, &details.Discriminator, &displayName, &avatar, &banner, &bot, &system, &flags, &presence); err != nil {
		log.Printf("Error fetching user details: %v", err)
		if err == gocql.ErrNotFound {
			return UserDetails{}, ErrUserNotFound
		}
		return UserDetails{}, err
	}
	status := "offline"
	if presence.Online {
		status = presence.Status
	}

	details.DisplayName = displayName
	details.Avatar = avatar
	details.Banner = banner
	details.Bot = bot
	details.System = system
	details.Flags = flags
	details.Presence = UserPresence{
		Status:       status,
		CustomStatus: presence.CustomStatus,
	}

	return details, nil
}

func (r *UserRepository) SetUserOnline(userID string) error {
	if r.session == nil {
		return ErrDatabaseNotInitialized
	}

	var presence Presence
	if err := r.session.Query("SELECT presence FROM users WHERE id = ?", userID).Scan(&presence); err != nil {
		log.Printf("Error getting current presence: %v", err)
		presence = Presence{
			Online:       false,
			Status:       "online",
			CustomStatus: "",
		}
	}

	presence.Online = true

	return r.session.Query("UPDATE users SET presence = ? WHERE id = ?",
		presence, userID).Exec()
}

func (r *UserRepository) SetUserOffline(userID string, sessionToken string) error {
	if r.session == nil {
		return ErrDatabaseNotInitialized
	}

	var presence Presence
	if err := r.session.Query("SELECT presence FROM users WHERE id = ?", userID).Scan(&presence); err != nil {
		log.Printf("Error getting current presence: %v", err)
		presence = Presence{
			Online:       false,
			Status:       "online",
			CustomStatus: "",
		}
	}

	presence.Online = false

	return r.session.Query("UPDATE users SET presence = ? WHERE id = ?",
		presence, userID).Exec()
}

type RelationshipType int

const (
	RelationshipTypeFriend RelationshipType = iota
	RelationshipTypePending
	RelationshipTypeOutgoing
)

type Relationship struct {
	ID          string    `json:"id"`
	SenderID    string    `json:"sender_id"`
	RecipientID string    `json:"recipient_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetRelatedUserIDs retrieves all user IDs that the given user has relationships with
func (r *UserRepository) GetRelatedUserIDs(userID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	relatedUsers := make(map[string]bool) // Using map to deduplicate IDs

	// Get IDs where user is sender (get recipient_ids)
	senderIter := r.session.Query("SELECT recipient_id FROM relationships_by_sender WHERE sender_id = ?", userID).Iter()
	var otherID string
	for senderIter.Scan(&otherID) {
		relatedUsers[otherID] = true
	}
	if err := senderIter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get relationships where user is sender: %v", err)
	}

	// Get IDs where user is recipient (get sender_ids)
	recipientIter := r.session.Query("SELECT sender_id FROM relationships_by_recipient WHERE recipient_id = ?", userID).Iter()
	for recipientIter.Scan(&otherID) {
		relatedUsers[otherID] = true
	}
	if err := recipientIter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get relationships where user is recipient: %v", err)
	}

	// Get accepted friends from relationships array
	relationships, err := r.GetUserRelationships(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accepted relationships: %v", err)
	}
	for _, friendID := range relationships {
		relatedUsers[friendID] = true
	}

	// Convert map keys to slice
	result := make([]string, 0, len(relatedUsers))
	for id := range relatedUsers {
		result = append(result, id)
	}

	return result, nil
}

// GetUserRelationshipRequests retrieves all pending relationship requests for a user
func (r *UserRepository) GetUserRelationshipRequests(userID string) ([]Relationship, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetUserRelationshipRequests] Getting requests for user: %s", userID)

	var relationships []Relationship

	// Get incoming requests (where user is recipient)
	query := "SELECT id, sender_id, recipient_id FROM relationships_by_recipient WHERE recipient_id = ?"
	log.Printf("[GetUserRelationshipRequests] Executing recipient query: %s with userID: %s", query, userID)

	recipientIter := r.session.Query(query, userID).Iter()

	var id, senderID, recipientID string

	for recipientIter.Scan(&id, &senderID, &recipientID) {
		log.Printf("[GetUserRelationshipRequests] Found incoming request: id=%s, sender=%s, recipient=%s",
			id, senderID, recipientID)

		relationships = append(relationships, Relationship{
			ID:          id,
			SenderID:    senderID,
			RecipientID: recipientID,
			CreatedAt:   time.Now(), // Default to current time since we don't have created_at in this table
		})
	}

	if err := recipientIter.Close(); err != nil {
		log.Printf("[GetUserRelationshipRequests] Error closing recipient iterator: %v", err)
		return nil, err
	}

	// Get outgoing requests (where user is sender)
	senderQuery := "SELECT id, sender_id, recipient_id FROM relationships_by_sender WHERE sender_id = ?"
	log.Printf("[GetUserRelationshipRequests] Executing sender query: %s with userID: %s", senderQuery, userID)

	senderIter := r.session.Query(senderQuery, userID).Iter()

	for senderIter.Scan(&id, &senderID, &recipientID) {
		log.Printf("[GetUserRelationshipRequests] Found outgoing request: id=%s, sender=%s, recipient=%s",
			id, senderID, recipientID)

		relationships = append(relationships, Relationship{
			ID:          id,
			SenderID:    senderID,
			RecipientID: recipientID,
			CreatedAt:   time.Now(), // Default to current time since we don't have created_at in this table
		})
	}

	if err := senderIter.Close(); err != nil {
		log.Printf("[GetUserRelationshipRequests] Error closing sender iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetUserRelationshipRequests] Found %d total requests for user %s", len(relationships), userID)
	log.Printf("[GetUserRelationshipRequests] Returning relationships: %+v", relationships)

	return relationships, nil
}

// GetUsersDetails retrieves details for multiple users
func (r *UserRepository) GetUsersDetails(userIDs []string) (map[string]UserDetails, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	userDetails := make(map[string]UserDetails)

	// Using a map to deduplicate user IDs
	uniqueIDs := make(map[string]bool)
	for _, id := range userIDs {
		uniqueIDs[id] = true
	}

	for userID := range uniqueIDs {
		details, err := r.GetUserDetails(userID)
		if err != nil {
			if err != ErrUserNotFound {
				log.Printf("Error fetching details for user %s: %v", userID, err)
			}
			continue
		}
		userDetails[userID] = details
	}

	return userDetails, nil
}

// GetUserRelationships retrieves all accepted relationships for a user from their relationships array
func (r *UserRepository) GetUserRelationships(userID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	var relationships []string
	if err := r.session.Query("SELECT relationships FROM users WHERE id = ?", userID).Scan(&relationships); err != nil {
		return nil, fmt.Errorf("failed to get user relationships: %v", err)
	}

	return relationships, nil
}

// SetUserPresence updates a user's presence status and custom status
func (r *UserRepository) SetUserPresence(userID string, status string, customStatus string) error {
	if r.session == nil {
		return ErrDatabaseNotInitialized
	}

	query := r.session.Query(`
		UPDATE users 
		SET presence = {
			online: true,
			status: ?,
			custom_status: ?
		}
		WHERE id = ?`,
		status, customStatus, userID)

	if err := query.Exec(); err != nil {
		log.Printf("Error updating user presence: %v", err)
		return fmt.Errorf("failed to update user presence: %v", err)
	}

	return nil
}

// Room represents a chat room structure
type Room struct {
	ID            string    `json:"id"`
	Creator       *string   `json:"creator,omitempty"` // null if DM, set if group
	Recipients    []string  `json:"recipients"`        // array of user IDs
	Type          int       `json:"type"`              // 0 = DM, 1 = Group DM, 2 = Server Channel
	LastMessageId string    `json:"last_message_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// GetUserRooms retrieves all rooms that a user is a member of
func (r *UserRepository) GetUserRooms(userID string) ([]Room, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetUserRooms] Getting rooms for user: %s", userID)

	// First get all room IDs for this user from room_recipients_by_user
	roomIDs := make([]string, 0)
	roomIDsQuery := "SELECT room_id FROM room_recipients_by_user WHERE user_id = ?"
	roomIDsIter := r.session.Query(roomIDsQuery, userID).Iter()

	var roomID string
	for roomIDsIter.Scan(&roomID) {
		roomIDs = append(roomIDs, roomID)
	}

	if err := roomIDsIter.Close(); err != nil {
		log.Printf("[GetUserRooms] Error closing room IDs iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetUserRooms] Found %d room IDs for user %s: %v", len(roomIDs), userID, roomIDs)

	// Now get the full room data for each room ID
	rooms := make([]Room, 0, len(roomIDs))
	for _, id := range roomIDs {
		var room Room
		var creator, lastMessageId *string
		var recipients []string
		var createdAt, updatedAt time.Time

		roomQuery := "SELECT id, creator, recipients, type, last_message_id, created_at, updated_at FROM rooms WHERE id = ?"
		if err := r.session.Query(roomQuery, id).Scan(&room.ID, &creator, &recipients, &room.Type, &lastMessageId, &createdAt, &updatedAt); err != nil {
			log.Printf("[GetUserRooms] Error fetching room details for room ID %s: %v", id, err)
			continue
		}

		room.Creator = creator
		room.Recipients = recipients
		if lastMessageId != nil {
			room.LastMessageId = *lastMessageId
		}
		room.CreatedAt = createdAt
		room.UpdatedAt = updatedAt

		rooms = append(rooms, room)
	}

	log.Printf("[GetUserRooms] Returning %d rooms for user %s", len(rooms), userID)
	return rooms, nil
}

func (r *UserRepository) GetRoomMembers(roomID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetRoomMembers] Getting members for room: %s", roomID)

	// Get recipients directly from the rooms table
	var recipients []string
	query := "SELECT recipients FROM rooms WHERE id = ?"
	if err := r.session.Query(query, roomID).Scan(&recipients); err != nil {
		log.Printf("[GetRoomMembers] Error fetching room recipients: %v", err)
		return nil, err
	}

	log.Printf("[GetRoomMembers] Found %d members for room %s", len(recipients), roomID)
	return recipients, nil
}

func (r *UserRepository) GetSession() *gocql.Session {
	return r.session
}
