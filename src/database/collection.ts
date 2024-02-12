import { CLIENT_RENEG_LIMIT } from "tls";
import { cassandra } from ".";
import { OpCodes } from "../config";
import { Presence, WebSocket } from "../types";
import { clients } from "..";

export const users = {
  updatePresence: async (client: WebSocket, presence: Partial<Presence>) => {
    const presenceUpdate = {
      status: presence.status ?? client.user.presence.status,
      status_text: presence.status_text ?? client.user.presence.status_text,
      online: presence.online ?? client.user.presence.online,
    };

    if (
      presenceUpdate.status === client.user.presence.status &&
      presenceUpdate.status_text === client.user.presence.status_text &&
      presenceUpdate.online === client.user.presence.online
    )
      return;

    await cassandra
      .execute(
        `
        UPDATE ${cassandra.keyspace}.users
        SET presence=?
        WHERE id=?;
        `,
        [presenceUpdate, client.user.id],
        { prepare: true }
      )
      .then(async () => {
          client.user.presence = presenceUpdate;

          let alreadySent: any[] = [];

        for (const id of client.user.space_ids || []) {
          const space_members = await cassandra.execute(`
            SELECT user_id FROM ${cassandra.keyspace}.space_members
            WHERE space_id=?`,
            [id]
          );

          for (const member of space_members.rows) {
            const membersWs = clients.get(member.get("user_id"))
            if (membersWs)
              for (const ws of membersWs) {
               if (alreadySent.includes(ws.user.id)) return;
                ws.send(
                  JSON.stringify({
                    op: OpCodes.DISPATCH,
                    event: "PRESENCE_UPDATE",
                    data: { user: client.user, presence: presenceUpdate },
                  })
                );
                alreadySent.push(ws.user.id)
              }
          }
        }
      })

      .catch((err) => {
        console.error(err);
      });
  },
};
