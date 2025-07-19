package utils

import (
	"strconv"
)

// Permission constants
const (
	// General permissions
	ADMINISTRATOR    = "ADMINISTRATOR"
	VIEW_ROOMS       = "VIEW_ROOMS"
	MANAGE_CHANNELS  = "MANAGE_CHANNELS"
	MANAGE_ROLES     = "MANAGE_ROLES"
	MANAGE_SPACE     = "MANAGE_SPACE"
	KICK_MEMBERS     = "KICK_MEMBERS"
	BAN_MEMBERS      = "BAN_MEMBERS"
	MANAGE_NICKNAMES = "MANAGE_NICKNAMES"
	MANAGE_WEBHOOKS  = "MANAGE_WEBHOOKS"
	VIEW_AUDIT_LOG   = "VIEW_AUDIT_LOG"

	// Text channel permissions
	VIEW_ROOM            = "VIEW_ROOM"
	SEND_MESSAGES        = "SEND_MESSAGES"
	MANAGE_MESSAGES      = "MANAGE_MESSAGES"
	READ_MESSAGE_HISTORY = "READ_MESSAGE_HISTORY"
	MENTION_EVERYONE     = "MENTION_EVERYONE"
	USE_EXTERNAL_EMOJIS  = "USE_EXTERNAL_EMOJIS"
	ADD_REACTIONS        = "ADD_REACTIONS"
	ATTACH_FILES         = "ATTACH_FILES"
	EMBED_LINKS          = "EMBED_LINKS"

	// Voice channel permissions
	CONNECT              = "CONNECT"
	SPEAK                = "SPEAK"
	MUTE_MEMBERS         = "MUTE_MEMBERS"
	DEAFEN_MEMBERS       = "DEAFEN_MEMBERS"
	MOVE_MEMBERS         = "MOVE_MEMBERS"
	USE_VOICE_ACTIVATION = "USE_VOICE_ACTIVATION"
	PRIORITY_SPEAKER     = "PRIORITY_SPEAKER"
	STREAM               = "STREAM"
)

// PermissionValue represents a permission as a bit flag
type PermissionValue int64

// Permission bit flags
const (
	PermissionAdministrator PermissionValue = 1 << iota
	PermissionViewRooms
	PermissionViewRoom
	PermissionManageChannels
	PermissionManageRoles
	PermissionManageSpace
	PermissionKickMembers
	PermissionBanMembers
	PermissionManageNicknames
	PermissionManageWebhooks
	PermissionViewAuditLog
	PermissionSendMessages
	PermissionManageMessages
	PermissionReadMessageHistory
	PermissionMentionEveryone
	PermissionUseExternalEmojis
	PermissionAddReactions
	PermissionAttachFiles
	PermissionEmbedLinks
	PermissionConnect
	PermissionSpeak
	PermissionMuteMembers
	PermissionDeafenMembers
	PermissionMoveMembers
	PermissionUseVoiceActivation
	PermissionPrioritySpeaker
	PermissionStream
)

// BitfieldToPermissions converts a bitfield to permission strings
func BitfieldToPermissions(bitfield PermissionValue) []string {
	var permissions []string

	if bitfield&PermissionAdministrator != 0 {
		permissions = append(permissions, ADMINISTRATOR)
	}
	if bitfield&PermissionViewRooms != 0 {
		permissions = append(permissions, VIEW_ROOMS)
	}
	if bitfield&PermissionViewRoom != 0 {
		permissions = append(permissions, VIEW_ROOM)
	}
	if bitfield&PermissionManageChannels != 0 {
		permissions = append(permissions, MANAGE_CHANNELS)
	}
	if bitfield&PermissionManageRoles != 0 {
		permissions = append(permissions, MANAGE_ROLES)
	}
	if bitfield&PermissionManageSpace != 0 {
		permissions = append(permissions, MANAGE_SPACE)
	}
	if bitfield&PermissionKickMembers != 0 {
		permissions = append(permissions, KICK_MEMBERS)
	}
	if bitfield&PermissionBanMembers != 0 {
		permissions = append(permissions, BAN_MEMBERS)
	}
	if bitfield&PermissionManageNicknames != 0 {
		permissions = append(permissions, MANAGE_NICKNAMES)
	}
	if bitfield&PermissionManageWebhooks != 0 {
		permissions = append(permissions, MANAGE_WEBHOOKS)
	}
	if bitfield&PermissionViewAuditLog != 0 {
		permissions = append(permissions, VIEW_AUDIT_LOG)
	}
	if bitfield&PermissionSendMessages != 0 {
		permissions = append(permissions, SEND_MESSAGES)
	}
	if bitfield&PermissionManageMessages != 0 {
		permissions = append(permissions, MANAGE_MESSAGES)
	}
	if bitfield&PermissionReadMessageHistory != 0 {
		permissions = append(permissions, READ_MESSAGE_HISTORY)
	}
	if bitfield&PermissionMentionEveryone != 0 {
		permissions = append(permissions, MENTION_EVERYONE)
	}
	if bitfield&PermissionUseExternalEmojis != 0 {
		permissions = append(permissions, USE_EXTERNAL_EMOJIS)
	}
	if bitfield&PermissionAddReactions != 0 {
		permissions = append(permissions, ADD_REACTIONS)
	}
	if bitfield&PermissionAttachFiles != 0 {
		permissions = append(permissions, ATTACH_FILES)
	}
	if bitfield&PermissionEmbedLinks != 0 {
		permissions = append(permissions, EMBED_LINKS)
	}
	if bitfield&PermissionConnect != 0 {
		permissions = append(permissions, CONNECT)
	}
	if bitfield&PermissionSpeak != 0 {
		permissions = append(permissions, SPEAK)
	}
	if bitfield&PermissionMuteMembers != 0 {
		permissions = append(permissions, MUTE_MEMBERS)
	}
	if bitfield&PermissionDeafenMembers != 0 {
		permissions = append(permissions, DEAFEN_MEMBERS)
	}
	if bitfield&PermissionMoveMembers != 0 {
		permissions = append(permissions, MOVE_MEMBERS)
	}
	if bitfield&PermissionUseVoiceActivation != 0 {
		permissions = append(permissions, USE_VOICE_ACTIVATION)
	}
	if bitfield&PermissionPrioritySpeaker != 0 {
		permissions = append(permissions, PRIORITY_SPEAKER)
	}
	if bitfield&PermissionStream != 0 {
		permissions = append(permissions, STREAM)
	}

	return permissions
}

// PermissionBitfieldFromString converts a string to permission bitfield
func PermissionBitfieldFromString(bitfieldStr string) (PermissionValue, error) {
	if bitfieldStr == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(bitfieldStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return PermissionValue(value), nil
}
