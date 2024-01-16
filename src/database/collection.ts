import { cassandra } from ".";
import { OpCodes } from "../config";
import { Presence, WebSocket } from "../types";

export const users = {
    updatePresence: async (client: WebSocket, presence: Partial<Presence>) => {

        const presenceUpdate = { status: presence.status ?? client.user.presence.status, status_text: presence.status_text ?? client.user.presence.status_text, online: presence.online ?? client.user.presence.online };

        if (presenceUpdate.status === client.user.presence.status && presenceUpdate.status_text === client.user.presence.status_text && presenceUpdate.online === client.user.presence.online) return;

        await cassandra.execute(`
        UPDATE ${cassandra.keyspace}.users
        SET presence=?
        WHERE id=? AND created_at=?;
        `, [presenceUpdate, client.user.id, client.user.created_at], { prepare: true }).then(() => {
            client.send(JSON.stringify({ op: OpCodes.DISPATCH, event: "PRESENCE_UPDATE", data: { user: client.user, presence: presenceUpdate } }));
            client.user.presence = presenceUpdate;
        }).catch((err) => {
            console.error(err);
        });
    }
}