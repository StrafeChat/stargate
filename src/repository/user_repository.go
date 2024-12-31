package repository

import (
	"errors"
	"log"
	"time"

	"github.com/gocql/gocql"
)

var (
	ErrDatabaseNotInitialized = errors.New("database connection not initialized")
	ErrUserNotFound          = errors.New("user not found")
	ErrTokenNotFound         = errors.New("token not found")
	ErrTokenExpired          = errors.New("token expired")
)

type Presence struct {
	Online       bool   `cql:"online"`
	Status       string `cql:"status"`
	CustomStatus string `cql:"custom_status"`
}

type UserPresence struct {
	Online       bool   `json:"online"`
	Status       string `json:"status"`
	CustomStatus string `json:"custom_status"`
}

type UserDetails struct {
	ID           string       `json:"id"`
	Username     string       `json:"username"`
	Discriminator int         `json:"discriminator"`
	Email        string       `json:"email"`
	DisplayName  string       `json:"display_name"`
	Avatar       string       `json:"avatar"`
	Banner       string       `json:"banner"`
	Bot          bool         `json:"bot"`
	System       bool         `json:"system"`
	Flags        int          `json:"flags"`
	Presence     UserPresence `json:"presence"`
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
	var email string
	var displayName string
	var avatar string
	var banner string
	var bot bool
	var system bool
	var flags int
	if err := r.session.Query("SELECT id, username, discriminator, email, display_name, avatar, banner, bot, system, flags, presence FROM users WHERE id = ?", 
		userID).Scan(&details.ID, &details.Username, &details.Discriminator, &email, &displayName, &avatar, &banner, &bot, &system, &flags, &presence); err != nil {
		log.Printf("Error fetching user details: %v", err)
		if err == gocql.ErrNotFound {
			return UserDetails{}, ErrUserNotFound
		}
		return UserDetails{}, err
	}

	details.Email = email
	details.DisplayName = displayName
	details.Avatar = avatar
	details.Banner = banner
	details.Bot = bot
	details.System = system
	details.Flags = flags
	details.Presence = UserPresence{
		Online:       presence.Online,
		Status:       presence.Status,
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
			Status:       "offline",
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
		return nil, err
	}

	// Get IDs where user is recipient (get sender_ids)
	recipientIter := r.session.Query("SELECT sender_id FROM relationships_by_recipient WHERE recipient_id = ?", userID).Iter()
	for recipientIter.Scan(&otherID) {
		relatedUsers[otherID] = true
	}
	if err := recipientIter.Close(); err != nil {
		return nil, err
	}

	// Convert map keys to slice
	userIDs := make([]string, 0, len(relatedUsers))
	for id := range relatedUsers {
		userIDs = append(userIDs, id)
	}

	return userIDs, nil
}

// GetUserRelationships retrieves all relationships for a user
func (r *UserRepository) GetUserRelationships(userID string) ([]Relationship, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	var relationships []Relationship

	// Query relationships where user is sender
	senderIter := r.session.Query("SELECT id, sender_id, recipient_id, created_at FROM relationships_by_sender WHERE sender_id = ?", userID).Iter()

	var id, senderID, recipientID string
	var createdAt time.Time

	for senderIter.Scan(&id, &senderID, &recipientID, &createdAt) {
		relationships = append(relationships, Relationship{
			ID:          id,
			SenderID:    senderID,
			RecipientID: recipientID,
			CreatedAt:   createdAt,
		})
	}

	if err := senderIter.Close(); err != nil {
		return nil, err
	}

	// Query relationships where user is recipient
	recipientIter := r.session.Query("SELECT id, sender_id, recipient_id, created_at FROM relationships_by_recipient WHERE recipient_id = ?", userID).Iter()

	for recipientIter.Scan(&id, &senderID, &recipientID, &createdAt) {
		relationships = append(relationships, Relationship{
			ID:          id,
			SenderID:    senderID,
			RecipientID: recipientID,
			CreatedAt:   createdAt,
		})
	}

	if err := recipientIter.Close(); err != nil {
		return nil, err
	}

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