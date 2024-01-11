import { WebSocketServer } from "ws";
import { ErrorCodes, ErrorMessages, HEARTBEAT, OpCodes, PORT } from "./config";
import database from "./database";
import { verifyToken } from "./helpers/validation";
import { WebSocket } from "./types";

const wss = new WebSocketServer({ port: PORT });

wss.on("listening", async () => {
    await database.init();
    console.log(`Stargate is listening on port ${PORT}`);
});

wss.on("connection", (client: WebSocket) => {

    client.on("open", () => {
        client.heartbeat = setTimeout(() => {
            client.close(ErrorCodes.SESSION_TIMED_OUT, ErrorMessages.SESSION_TIMED_OUT);
        }, 5000);
    });

    client.on("message", async (message) => {
        try {
            var { op, data } = JSON.parse(message.toString("utf-8"));
        } catch (err) {
            client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);
        }

        if (!op && !data) client.close(ErrorCodes.DECODE_ERROR, ErrorMessages.DECODE_ERROR);

        try {
            switch (op) {
                case OpCodes.IDENTIFY:
                    const response = await verifyToken(client, data.token);
                    if (typeof response == "string") client.id = response;
                    console.log("IDENTIFY");
                    break;
                case OpCodes.HEARTBEAT:
                    // console.log("HEARTBEAT");
                    // client.heartbeat?.refresh();
                    // if (!client.id) client.close(ErrorCodes.NOT_AUTHENTICATED, ErrorMessages.NOT_AUTHENTICATED);
                    break;
                default:
                    client.close(ErrorCodes.UNKNOWN_OPCODE, "You sent an invalid gateway opcode, so you have been disconnected.");
                    break;
            }
        } catch (err) {
            console.log(err);
            client.close(ErrorCodes.UNKNOWN, "Something went wrong, maybe try reconnecting?");
        }
    });
});