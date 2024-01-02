import ws from "ws";

export interface WebSocket extends ws.WebSocket {
    id?: string;
}