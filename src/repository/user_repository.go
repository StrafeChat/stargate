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
	if err := r.session.Query("SELECT id, username, presence FROM users WHERE id = ?", 
		userID).Scan(&details.ID, &details.Username, &presence); err != nil {
		log.Printf("Error fetching user details: %v", err)
		if err == gocql.ErrNotFound {
			return UserDetails{}, ErrUserNotFound
		}
		return UserDetails{}, err
	}

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