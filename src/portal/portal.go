package portal

import (
	"log"
	"slices"
)

var (
	participants map[string]([]string)
)

func SyncData(data map[string]map[string]([]string)) {
	rooms, ok := data["rooms"]
	if !ok {
		log.Printf("[VoiceSync] No room data found in voice sync data: %v", data)
		return
	}

	for room := range rooms {
		ps, ok := rooms[room]
		if !ok {
			log.Printf("[VoiceSync] Invalid room: %v, rooms: %v", ps, rooms)
			return
		}
		participants[room] = ps
	}

	log.Printf("[VoiceSync] Synced data: %v", participants)
}

func RoomClosed(room string) {
	if _, ok := participants[room]; !ok {
		delete(participants, room)
	}
}
func RegisterJoin(room, participant string) {
	p, ok := participants[room]
	if !ok {
		p = make([]string, 0)
	}

	if slices.Contains(p, participant) {
		return
	}

	p = append(p, participant)
	participants[room] = p
}
func RegisterLeave(room, participant string) {
	p, ok := participants[room]
	if !ok {
		return
	}

	idx := slices.IndexFunc(p, func(id string) bool {
		return id == participant
	})
	if idx == -1 {
		return
	}
	p = append(p[:idx], p[idx+1:]...)
	participants[room] = p
}
func GetParticipants(room string) []string {
	p, ok := participants[room]
	if !ok {
		p = make([]string, 0)
		participants[room] = p
		return p
	}
	return p
}
