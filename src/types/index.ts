import ws from "ws";

export interface WebSocket extends ws.WebSocket {
    user: User;
    heartbeat?: NodeJS.Timeout;
    verified?: boolean;
}

export interface Presence {
    status: "online" | "offline" | "idle" | "dnd";
    status_text: string;
    online: boolean;
}

export interface User {
    id: string;
    created_at: Date;
    presence: Presence
    username: string;
}

export interface Space {
    id: string;
    name: string;
    nameAcronym: string;
    icon: string | null;
    owner_id: string;
    afk_room_id: string;
    afk_timeout: number;
    verifcation_level: number;
    room_ids: string[];
    role_ids: string[];
    rules_room_id: string;
    description: string;
    banner: string;
    preferred_locale: string;
    sticker_ids: string[];
    emoji_ids: string[];
    created_at: number;
    edited_at: number;
}