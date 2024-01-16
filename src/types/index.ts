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