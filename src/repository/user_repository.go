package repository

import (
	"errors"
	"fmt"
	"log"
	"strconv"
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
	Email         *string      `json:"email"`
	Avatar        string       `json:"avatar"`
	Banner        string       `json:"banner"`
	Bot           bool         `json:"bot"`
	System        bool         `json:"system"`
	Bio           string       `json:"bio"`
	AboutMe       string       `json:"about_me"`
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
	var expiresAt time.Time
	query := r.session.Query("SELECT user_id, expires_at FROM sessions WHERE session_token = ?", token)

	if err := query.Scan(&userID, &expiresAt); err != nil {
		if err == gocql.ErrNotFound {
			log.Printf("Token not found: %s", token)
			return "", ErrTokenNotFound
		}
		log.Printf("Error validating token: %v", err)
		return "", err
	}

	if expiresAt.Before(time.Now()) {
		log.Printf("Token expired: %s", token)
		return "", ErrTokenExpired
	}

	log.Printf("Token valid for user: %s", userID)
	return userID, nil
}

func (r *UserRepository) GetUserDetailsWithoutEmail(userID string) (UserDetails, error) {
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
	var bio string
	var aboutMe string
	var flags int
	if err := r.session.Query("SELECT id, username, discriminator, display_name, avatar, banner, bot, system, bio, about_me, flags, presence FROM users WHERE id = ?",
		userID).Scan(&details.ID, &details.Username, &details.Discriminator, &displayName, &avatar, &banner, &bot, &system, &bio, &aboutMe, &flags, &presence); err != nil {
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
	// Email is intentionally not set for other users
	details.Avatar = avatar
	details.Banner = banner
	details.Bot = bot
	details.System = system
	details.Bio = bio
	details.AboutMe = aboutMe
	details.Flags = flags
	details.Presence = UserPresence{
		Status:       status,
		CustomStatus: presence.CustomStatus,
	}

	return details, nil
}

func (r *UserRepository) GetUserDetails(userID string) (UserDetails, error) {
	if r.session == nil {
		return UserDetails{}, ErrDatabaseNotInitialized
	}

	var details UserDetails
	var presence Presence
	var displayName string
	var email *string
	var avatar string
	var banner string
	var bot bool
	var system bool
	var bio string
	var aboutMe string
	var flags int
	if err := r.session.Query("SELECT id, username, discriminator, display_name, email, avatar, banner, bot, system, bio, about_me, flags, presence FROM users WHERE id = ?",
		userID).Scan(&details.ID, &details.Username, &details.Discriminator, &displayName, &email, &avatar, &banner, &bot, &system, &bio, &aboutMe, &flags, &presence); err != nil {
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
	details.Email = email
	details.Avatar = avatar
	details.Banner = banner
	details.Bot = bot
	details.System = system
	details.Bio = bio
	details.AboutMe = aboutMe
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
		details, err := r.GetUserDetailsWithoutEmail(userID)
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
	Creator       *string   `json:"creator,omitempty"`  // null if DM, set if group
	Recipients    []string  `json:"recipients"`         // array of user IDs
	Type          int       `json:"type"`               // 0 = DM, 1 = Group DM, 2 = Server Room, 3 = Space Room, 4 = Space Section
	SpaceID       *string   `json:"space_id,omitempty"` // space ID for space rooms and sections
	Position      *int      `json:"position"`
	ParentID      *string   `json:"parent_id,omitempty"` // parent section ID if this room belongs to a section
	Name          *string   `json:"name,omitempty"`      // optional name for group PMs
	Topic         *string   `json:"topic,omitempty"`     // optional topic for group PMs
	Icon          *string   `json:"icon,omitempty"`      // optional icon for group PMs
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

	// First get all room IDs for this user from room_recipients_by_user (DMs and group chats)
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

	log.Printf("[GetUserRooms] Found %d direct room IDs for user %s: %v", len(roomIDs), userID, roomIDs)

	// Also get rooms from spaces the user is a member of
	spaceIDs := make([]int64, 0)
	spaceIDsQuery := "SELECT space_id FROM space_members_by_user WHERE user_id = ?"
	spaceIDsIter := r.session.Query(spaceIDsQuery, userID).Iter()

	var spaceIDStr string
	for spaceIDsIter.Scan(&spaceIDStr) {
		if spaceID, err := strconv.ParseInt(spaceIDStr, 10, 64); err == nil {
			spaceIDs = append(spaceIDs, spaceID)
		} else {
			log.Printf("[GetUserRooms] Error parsing space ID %s: %v", spaceIDStr, err)
		}
	}

	if err := spaceIDsIter.Close(); err != nil {
		log.Printf("[GetUserRooms] Error closing space IDs iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetUserRooms] Found %d space IDs for user %s: %v", len(spaceIDs), userID, spaceIDs)

	// Get rooms that belong to these spaces
	for _, spaceID := range spaceIDs {
		spaceRoomsQuery := "SELECT id FROM rooms WHERE space_id = ? ALLOW FILTERING"
		spaceRoomsIter := r.session.Query(spaceRoomsQuery, spaceID).Iter()

		var spaceRoomID string
		for spaceRoomsIter.Scan(&spaceRoomID) {
			roomIDs = append(roomIDs, spaceRoomID)
		}

		if err := spaceRoomsIter.Close(); err != nil {
			log.Printf("[GetUserRooms] Error closing space rooms iterator: %v", err)
		}
	}

	log.Printf("[GetUserRooms] Found %d total room IDs for user %s (including space rooms): %v", len(roomIDs), userID, roomIDs)

	// Now get the full room data for each room ID
	rooms := make([]Room, 0, len(roomIDs))
	for _, id := range roomIDs {
		var room Room
		var creator, name, topic, icon, lastMessageId *string
		var spaceID *int64
		var parentID *string
		var position *int
		var recipients []string
		var createdAt, updatedAt time.Time

		roomQuery := "SELECT id, creator, recipients, type, space_id, parent_id, position, name, topic, icon, last_message_id, created_at, updated_at FROM rooms WHERE id = ?"
		if err := r.session.Query(roomQuery, id).Scan(&room.ID, &creator, &recipients, &room.Type, &spaceID, &parentID, &position, &name, &topic, &icon, &lastMessageId, &createdAt, &updatedAt); err != nil {
			log.Printf("[GetUserRooms] Error fetching room details for room ID %s: %v", id, err)
			continue
		}

		room.Creator = creator
		room.Recipients = recipients
		// Convert spaceID from *int64 to *string
		if spaceID != nil {
			spaceIDStr := strconv.FormatInt(*spaceID, 10)
			room.SpaceID = &spaceIDStr
		} else {
			room.SpaceID = nil
		}
		room.ParentID = parentID
		room.Position = position
		room.Name = name
		room.Topic = topic
		room.Icon = icon
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

// GetRoomMembersWithPermissions gets room members based on room type and permissions
func (r *UserRepository) GetRoomMembersWithPermissions(roomID string, requiredPermission string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetRoomMembersWithPermissions] Getting members for room: %s with permission: %s", roomID, requiredPermission)

	// First get room details including type and space_id
	var roomType int
	var spaceID *int64
	var recipients []string
	roomQuery := "SELECT type, space_id, recipients FROM rooms WHERE id = ?"
	if err := r.session.Query(roomQuery, roomID).Scan(&roomType, &spaceID, &recipients); err != nil {
		log.Printf("[GetRoomMembersWithPermissions] Error fetching room details: %v", err)
		return nil, err
	}

	// For PM (0) and Group PM (1), return recipients directly
	if roomType == 0 || roomType == 1 {
		log.Printf("[GetRoomMembersWithPermissions] Room is PM/Group PM, returning %d recipients", len(recipients))
		return recipients, nil
	}

	// For space rooms (2, 3, 4), check space member permissions
	if spaceID == nil {
		log.Printf("[GetRoomMembersWithPermissions] Space room has no space_id")
		return []string{}, nil
	}

	spaceIDStr := strconv.FormatInt(*spaceID, 10)
	log.Printf("[GetRoomMembersWithPermissions] Checking permissions for space: %s", spaceIDStr)

	// Get all space members
	spaceMembers, err := r.GetSpaceMembers(spaceIDStr)
	if err != nil {
		log.Printf("[GetRoomMembersWithPermissions] Error getting space members: %v", err)
		return nil, err
	}

	// Filter members based on permissions
	var authorizedMembers []string
	for _, memberID := range spaceMembers {
		hasPermission, err := r.CheckSpaceMemberPermission(spaceIDStr, memberID, requiredPermission)
		if err != nil {
			log.Printf("[GetRoomMembersWithPermissions] Error checking permission for user %s: %v", memberID, err)
			continue
		}
		if hasPermission {
			authorizedMembers = append(authorizedMembers, memberID)
		}
	}

	log.Printf("[GetRoomMembersWithPermissions] Found %d authorized members for room %s", len(authorizedMembers), roomID)
	return authorizedMembers, nil
}

// CheckSpaceMemberPermission checks if a user has a specific permission in a space
func (r *UserRepository) CheckSpaceMemberPermission(spaceID, userID, permission string) (bool, error) {
	if r.session == nil {
		return false, ErrDatabaseNotInitialized
	}

	log.Printf("[CheckSpaceMemberPermission] Checking permission %s for user %s in space %s", permission, userID, spaceID)

	// Convert spaceID to int64
	spaceIDInt, err := strconv.ParseInt(spaceID, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid space ID: %v", err)
	}

	// Get user's roles in the space
	userRoles, err := r.GetMemberRolesFromJunctionTable(spaceIDInt, userID)
	if err != nil {
		log.Printf("[CheckSpaceMemberPermission] Error getting user roles: %v", err)
		return false, err
	}

	if len(userRoles) == 0 {
		log.Printf("[CheckSpaceMemberPermission] User %s has no roles in space %s", userID, spaceID)
		return false, nil
	}

	// Check if any of the user's roles have the required permission
	for _, roleID := range userRoles {
		// Get role permissions
		var permissions []string
		roleQuery := "SELECT permissions FROM space_roles WHERE space_id = ? AND role_id = ?"
		if err := r.session.Query(roleQuery, spaceIDInt, roleID).Scan(&permissions); err != nil {
			log.Printf("[CheckSpaceMemberPermission] Error getting role permissions for role %s: %v", roleID, err)
			continue
		}

		// Check if the required permission is in the role's permissions
		for _, perm := range permissions {
			if perm == permission {
				log.Printf("[CheckSpaceMemberPermission] User %s has permission %s via role %s", userID, permission, roleID)
				return true, nil
			}
		}
	}

	log.Printf("[CheckSpaceMemberPermission] User %s does not have permission %s in space %s", userID, permission, spaceID)
	return false, nil
}

// Space represents a space structure
type Space struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	NameAcronym string    `json:"name_acronym"`
	Description *string   `json:"description,omitempty"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetUserSpaces retrieves all spaces that a user is a member of
func (r *UserRepository) GetUserSpaces(userID string) ([]Space, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetUserSpaces] Getting spaces for user: %s", userID)

	// First get all space IDs for this user from space_members_by_user
	spaceIDs := make([]int64, 0)
	spaceIDsQuery := "SELECT space_id FROM space_members_by_user WHERE user_id = ?"
	spaceIDsIter := r.session.Query(spaceIDsQuery, userID).Iter()

	var spaceIDStr string
	for spaceIDsIter.Scan(&spaceIDStr) {
		// Convert string to int64
		if spaceID, err := strconv.ParseInt(spaceIDStr, 10, 64); err == nil {
			spaceIDs = append(spaceIDs, spaceID)
		} else {
			log.Printf("[GetUserSpaces] Error parsing space ID %s: %v", spaceIDStr, err)
		}
	}

	if err := spaceIDsIter.Close(); err != nil {
		log.Printf("[GetUserSpaces] Error closing space IDs iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetUserSpaces] Found %d space IDs for user %s: %v", len(spaceIDs), userID, spaceIDs)

	// Now get the full space data for each space ID
	spaces := make([]Space, 0, len(spaceIDs))
	for _, id := range spaceIDs {
		log.Printf("[GetUserSpaces] Fetching details for space ID %d", id)

		var space Space
		spaceDetailsQuery := "SELECT id, name, name_acronym, description, owner_id, created_at, updated_at FROM spaces WHERE id = ?"
		err := r.session.Query(spaceDetailsQuery, id).Scan(
			&space.ID, &space.Name, &space.NameAcronym, &space.Description, &space.OwnerID, &space.CreatedAt, &space.UpdatedAt,
		)
		if err != nil {
			log.Printf("[GetUserSpaces] Error fetching space details for space ID %d: %v", id, err)
			continue
		}

		spaces = append(spaces, space)
		log.Printf("[GetUserSpaces] Successfully fetched space: %s (ID: %s)", space.Name, space.ID)
	}

	log.Printf("[GetUserSpaces] Returning %d spaces for user %s", len(spaces), userID)
	return spaces, nil
}

// GetRoomSpaceID retrieves the space ID for a given room
func (r *UserRepository) GetRoomSpaceID(roomID string) (*string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetRoomSpaceID] Getting space ID for room: %s", roomID)

	var spaceID *int64
	query := "SELECT space_id FROM rooms WHERE id = ?"
	if err := r.session.Query(query, roomID).Scan(&spaceID); err != nil {
		log.Printf("[GetRoomSpaceID] Error fetching space ID for room %s: %v", roomID, err)
		return nil, err
	}

	if spaceID == nil {
		log.Printf("[GetRoomSpaceID] Room %s has no space ID (likely a DM or group chat)", roomID)
		return nil, nil
	}

	spaceIDStr := strconv.FormatInt(*spaceID, 10)
	log.Printf("[GetRoomSpaceID] Room %s belongs to space %s", roomID, spaceIDStr)
	return &spaceIDStr, nil
}

// GetSpaceMembers retrieves all members of a specific space
func (r *UserRepository) GetSpaceMembers(spaceID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetSpaceMembers] Getting members for space: %s", spaceID)

	// Convert spaceID string to int64
	spaceIDInt, err := strconv.ParseInt(spaceID, 10, 64)
	if err != nil {
		log.Printf("[GetSpaceMembers] Error parsing space ID %s: %v", spaceID, err)
		return nil, fmt.Errorf("invalid space ID: %v", err)
	}

	// Get all user IDs for this space from space_members table
	var userIDs []string
	query := "SELECT user_id FROM space_members WHERE space_id = ?"
	iter := r.session.Query(query, spaceIDInt).Iter()

	var userID string
	for iter.Scan(&userID) {
		userIDs = append(userIDs, userID)
	}

	if err := iter.Close(); err != nil {
		log.Printf("[GetSpaceMembers] Error closing iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetSpaceMembers] Found %d members for space %s", len(userIDs), spaceID)
	return userIDs, nil
}

// SpaceMember represents a space member with roles and user details
type SpaceMember struct {
	SpaceID     string      `json:"space_id"`
	UserID      string      `json:"user_id"`
	Nick        *string     `json:"nick,omitempty"`
	Avatar      *string     `json:"avatar,omitempty"`
	Roles       []string    `json:"roles"`
	JoinedAt    time.Time   `json:"joined_at"`
	Deaf        bool        `json:"deaf"`
	Mute        bool        `json:"mute"`
	Flags       int         `json:"flags"`
	Pending     bool        `json:"pending"`
	User        UserDetails `json:"user"`
}

// SpaceRole represents a space role
type SpaceRole struct {
	SpaceID     string    `json:"space_id"`
	RoleID      string    `json:"role_id"`
	Name        string    `json:"name"`
	Color       *string   `json:"color,omitempty"`
	Permissions []string  `json:"permissions"`
	Position    int       `json:"position"`
	Mentionable bool      `json:"mentionable"`
	Hoist       bool      `json:"hoist"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetSpaceMembersWithRoles retrieves all members of a specific space with their roles and user details
func (r *UserRepository) GetSpaceMembersWithRoles(spaceID string) ([]SpaceMember, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetSpaceMembersWithRoles] Getting members with roles for space: %s", spaceID)

	// Convert spaceID string to int64
	spaceIDInt, err := strconv.ParseInt(spaceID, 10, 64)
	if err != nil {
		log.Printf("[GetSpaceMembersWithRoles] Error parsing space ID %s: %v", spaceID, err)
		return nil, fmt.Errorf("invalid space ID: %v", err)
	}

	// Get all members for this space from space_members table (without roles field)
	var members []SpaceMember
	query := "SELECT space_id, user_id, nick, avatar, joined_at, deaf, mute, flags, pending FROM space_members WHERE space_id = ?"
	iter := r.session.Query(query, spaceIDInt).Iter()

	var member SpaceMember
	var spaceIDResult int64
	for iter.Scan(&spaceIDResult, &member.UserID, &member.Nick, &member.Avatar, &member.JoinedAt, &member.Deaf, &member.Mute, &member.Flags, &member.Pending) {
		member.SpaceID = strconv.FormatInt(spaceIDResult, 10)
		
		// Fetch roles from the new junction table
		memberRoles, err := r.GetMemberRolesFromJunctionTable(spaceIDInt, member.UserID)
		if err != nil {
			log.Printf("[GetSpaceMembersWithRoles] Error fetching roles for user %s in space %s: %v", member.UserID, spaceID, err)
			// Continue with empty roles rather than failing completely
			member.Roles = []string{}
		} else {
			member.Roles = memberRoles
		}
		
		// Fetch user details for this member
		userDetails, err := r.GetUserDetailsWithoutEmail(member.UserID)
		if err != nil {
			log.Printf("[GetSpaceMembersWithRoles] Error fetching user details for user %s: %v", member.UserID, err)
			// Continue with empty user details rather than failing completely
			member.User = UserDetails{ID: member.UserID}
		} else {
			member.User = userDetails
		}
		
		members = append(members, member)
	}

	if err := iter.Close(); err != nil {
		log.Printf("[GetSpaceMembersWithRoles] Error closing iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetSpaceMembersWithRoles] Found %d members with roles for space %s", len(members), spaceID)
	return members, nil
}

// GetSpaceRoles retrieves all roles for a specific space
func (r *UserRepository) GetSpaceRoles(spaceID string) ([]SpaceRole, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetSpaceRoles] Getting roles for space: %s", spaceID)

	// Convert spaceID string to int64
	spaceIDInt, err := strconv.ParseInt(spaceID, 10, 64)
	if err != nil {
		log.Printf("[GetSpaceRoles] Error parsing space ID %s: %v", spaceID, err)
		return nil, fmt.Errorf("invalid space ID: %v", err)
	}

	// Get all roles for this space from space_roles table
	var roles []SpaceRole
	query := "SELECT space_id, role_id, name, color, permissions, position, mentionable, hoist, created_at, updated_at FROM space_roles WHERE space_id = ?"
	iter := r.session.Query(query, spaceIDInt).Iter()

	var role SpaceRole
	var spaceIDResult int64
	for iter.Scan(&spaceIDResult, &role.RoleID, &role.Name, &role.Color, &role.Permissions, &role.Position, &role.Mentionable, &role.Hoist, &role.CreatedAt, &role.UpdatedAt) {
		role.SpaceID = strconv.FormatInt(spaceIDResult, 10)
		roles = append(roles, role)
	}

	if err := iter.Close(); err != nil {
		log.Printf("[GetSpaceRoles] Error closing iterator: %v", err)
		return nil, err
	}

	log.Printf("[GetSpaceRoles] Found %d roles for space %s", len(roles), spaceID)
	return roles, nil
}

// GetMemberRolesFromJunctionTable retrieves member roles from the new junction table system
func (r *UserRepository) GetMemberRolesFromJunctionTable(spaceID int64, userID string) ([]string, error) {
	if r.session == nil {
		return nil, ErrDatabaseNotInitialized
	}

	log.Printf("[GetMemberRolesFromJunctionTable] Getting roles for user %s in space %d", userID, spaceID)

	// Query the space_member_roles junction table
	var roleIDs []string
	query := "SELECT role_id FROM space_member_roles WHERE space_id = ? AND user_id = ?"
	iter := r.session.Query(query, spaceID, userID).Iter()

	var roleID string
	for iter.Scan(&roleID) {
		roleIDs = append(roleIDs, roleID)
	}

	if err := iter.Close(); err != nil {
		log.Printf("[GetMemberRolesFromJunctionTable] Error closing iterator: %v", err)
		return nil, err
	}

	// Always include @everyone role if not already present
	hasEveryoneRole := false
	for _, id := range roleIDs {
		if id == "@everyone" {
			hasEveryoneRole = true
			break
		}
	}
	if !hasEveryoneRole {
		roleIDs = append([]string{"@everyone"}, roleIDs...)
	}

	log.Printf("[GetMemberRolesFromJunctionTable] Found %d roles for user %s in space %d: %v", len(roleIDs), userID, spaceID, roleIDs)
	return roleIDs, nil
}

func (r *UserRepository) GetSession() *gocql.Session {
	return r.session
}
