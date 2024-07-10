import http from "http";
import { WebSocketServer } from "ws";
import { ErrorCodes, ErrorMessages, HEARTBEAT, OpCodes, PORT } from "./config";
import database, { redis, cassandra } from "./database";
import { users } from "./database/collection";
import { verifyToken, dataRevaluation } from "./helpers/validation";
import { WebSocket } from "./types";

const server = http.createServer();
const wss = new WebSocketServer({ noServer: true });
const clients = new Map<string, WebSocket[]>();

export interface UserInfo {
  id: string
}

const voiceUsers = new Map<string, UserInfo[]>();

wss.on("error", (err) => {
  console.error(err);
});

wss.on("connection", (client: WebSocket) => {
  client.send(
    JSON.stringify({
      op: OpCodes.HELLO,
      data: {
        heartbeat_interval: HEARTBEAT,
      },
    })
  );

  client.heartbeat = setTimeout(() => {
    client.close(ErrorCodes.SESSION_TIMED_OUT, ErrorMessages.SESSION_TIMED_OUT);
  }, HEARTBEAT + 1500);

  client.on("message", async (message) => {
    try {
      var { op, data } = JSON.parse(message.toString("utf-8")) as {
        op: OpCodes;
        data: any;
      };
    } catch (err) {
      console.error(err);
      return client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);
    }

    if (!op && !data)
      return client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);

    try {
      switch (op) {
        case OpCodes.IDENTIFY:
          await verifyToken(client, data.token, voiceUsers);
          if (client.verified)
            setTimeout(async () => {
              await users.updatePresence(client, {
                ...client.user.presence,
                online: true,
              });
            }, 100);
          const clts = clients.get(client.user.id);
          if (!clts) clients.set(client.user.id, [client]);
          else clients.get(client.user.id)?.push(client);

          break;
        case OpCodes.REFRESH:
          await dataRevaluation(client)
        case OpCodes.HEARTBEAT:
          if (!client.verified)
            client.close(
              ErrorCodes.NOT_AUTHENTICATED,
              ErrorMessages.NOT_AUTHENTICATED
            );
          else { client.heartbeat?.refresh();
            console.info(`[CLIENT] ${client.user.username} has sent a heartbeat.`)
          }
          break;
        case OpCodes.PRESENCE:
          if (!client.verified)
            client.close(
              ErrorCodes.NOT_AUTHENTICATED,
              ErrorMessages.NOT_AUTHENTICATED
            );
          await users.updatePresence(client, data);
          break;
        default:
          client.close(
            ErrorCodes.UNKNOWN_OPCODE,
            "You sent an invalid gateway opcode, so you have been disconnected."
          );
          break;
      }
    } catch (err) {
      console.error(err);
      client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.UNKNOWN);
    }
  });

  client.on("close", async () => {
    clearTimeout(client.heartbeat);
    const clientsArray = clients.get(client.user?.id);

    if (clientsArray) {
      const index = clientsArray.indexOf(client);
      if (index !== -1) clientsArray.splice(index, 1);
    }

    if (clientsArray?.length === 0) {
      if (client.verified)
        await users.updatePresence(client, {
          ...client.user.presence,
          online: false,
        });
      clients.delete(client.user?.id);
    }

    client.terminate();
  });
});

server.on("upgrade", (req, socket, head) => {
  wss.handleUpgrade(req, socket, head, (ws) => {
    wss.emit("connection", ws, req);
  });
});

server.listen(PORT, "0.0.0.0", async () => {
  await database.init();
  redis.subscribe("stargate", async (res) => {
    const { event, data } = JSON.parse(res);
    const space_members = await cassandra.execute(`
      SELECT user_id FROM ${cassandra.keyspace}.space_members
      WHERE space_id=?`,
      [data.space_id]
    );

    if (event.toUpperCase() === "VOICE_JOIN") {
      if (!voiceUsers.has(data.room)) voiceUsers.set(data.room, []);
      const room = voiceUsers.get(data.room)!;
      room.push({
        id: data.user
      });
      voiceUsers.set(data.room, room);
    }
    if (event.toUpperCase() === "VOICE_LEAVE") {
      if (voiceUsers.has(data.room)) {
        const room = voiceUsers.get(data.room)!;
        const idx = room.findIndex(e => {
          return e.id === data.user
        });
        if (idx > -1) {
          room.splice(idx, 1);
          voiceUsers.set(data.room, room);
        }
      }
    }

    for (const member of space_members.rows) {
      const membersWs = clients.get(member.get("user_id"))
      if (membersWs) {
        for (const ws of membersWs) {
          ws.send(
            JSON.stringify({
              op: OpCodes.DISPATCH,
              event: event.toUpperCase(),
              data: data,
            })
          );
        }
      }
    }
  });
  redis.subscribe("stargate_personal", async (res) => {
    const { event, data, users } = JSON.parse(res);

    for (const user of users) {
      if (!clients.has(user)) return;
      const membersWs = clients.get(user)!;
      for (const ws of membersWs) {
        ws.send(
          JSON.stringify({
            op: OpCodes.DISPATCH,
            event: event.toUpperCase(),
            data: data,
          })
        );
      }
    }
  });
  console.info("Listening on port " + PORT);
});

export { clients };
