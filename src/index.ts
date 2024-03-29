import http from "http";
import { WebSocketServer } from "ws";
import { ErrorCodes, ErrorMessages, HEARTBEAT, OpCodes, PORT } from "./config";
import database, { cassandra } from "./database";
import { users } from "./database/collection";
import { verifyToken } from "./helpers/validation";
import { WebSocket } from "./types";

const server = http.createServer();
const wss = new WebSocketServer({ noServer: true });

wss.on("error", (err) => {
    console.error(err);
})

wss.on("connection", (client: WebSocket) => {

    client.send(JSON.stringify({
        op: OpCodes.HELLO,
        data: {
            heartbeat_interval: HEARTBEAT
        }
    }));

    client.heartbeat = setTimeout(() => {
        client.close(ErrorCodes.SESSION_TIMED_OUT, ErrorMessages.SESSION_TIMED_OUT);
    }, HEARTBEAT + 500);

    client.on("message", async (message) => {
        try {
            var { op, data } = JSON.parse(message.toString("utf-8")) as { op: OpCodes, data: any };
        } catch (err) {
            console.error(err);
            return client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);
        }

        if (!op && !data) return client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);

        try {
            switch (op) {
                case OpCodes.IDENTIFY:
                    await verifyToken(client, data.token);
                    if (client.verified) await users.updatePresence(client, { ...client.user.presence, online: true })
                    break;
                case OpCodes.HEARTBEAT:
                    if (!client.verified) client.close(ErrorCodes.NOT_AUTHENTICATED, ErrorMessages.NOT_AUTHENTICATED);
                    else client.heartbeat?.refresh();
                    break;
                case OpCodes.PRESENCE:
                    if (!client.verified) client.close(ErrorCodes.NOT_AUTHENTICATED, ErrorMessages.NOT_AUTHENTICATED);
                    await users.updatePresence(client, data);
                    break;
                default:
                    client.close(ErrorCodes.UNKNOWN_OPCODE, "You sent an invalid gateway opcode, so you have been disconnected.");
                    break;
            }
        } catch (err) {
            console.error(err);
            client.close(ErrorCodes.UNKNOWN, ErrorMessages.UNKNOWN);
        }
    })

    client.on("close", async () => {
        if (client.verified) await users.updatePresence(client, { ...client.user.presence, online: false });
        clearTimeout(client.heartbeat);
        client.terminate();
    });
});

server.on("upgrade", (req, socket, head) => {
    wss.handleUpgrade(req, socket, head, (ws) => {
        wss.emit("connection", ws, req);
    })
});

server.listen(PORT, async () => {
    await database.init();
    console.info("Listening on port " + PORT);
});