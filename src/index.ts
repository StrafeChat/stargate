import { WebSocketServer } from "ws";
import { ErrorCodes, ErrorMessages, OpCodes, PORT } from "./config";
import database from "./database";
import { verifyToken } from "./helpers/validation";
import { WebSocket } from "./types";

const wss = new WebSocketServer({ port: PORT });

wss.on("listening", async () => {
    await database.init();
    console.log(`Stargate is listening on port ${PORT}`);
});

wss.on("connection", (client: WebSocket) => {
    console.log(client);
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
                    break;
                default:
                    client.close(ErrorCodes.UNKNOWN_OPCODE, "You sent an invalid gateway opcode, so you have been disconnected.");
                    break;
            }
        } catch (err) {
            client.close(ErrorCodes.UNKNOWN, "Something went wrong, maybe try reconnecting?");
        }
    });
});