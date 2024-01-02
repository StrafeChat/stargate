import { ErrorCodes, ErrorMessages } from "../config";
import { cassandra } from "../database";
import { WebSocket } from "../types";

export const verifyToken = async (client: WebSocket, token: string) => {

    if (typeof token != "string") return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    const splitToken = token.split('.');

    if (splitToken.length < 3) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    const id = atob(splitToken[0]);
    const timestamp = atob(splitToken[1]);
    // TODO: This will be the session id in the future
    const secret = atob(splitToken[2]);

    const user = await cassandra.execute(`
    SELECT last_pass_reset, secret FROM ${cassandra.keyspace}.users
    WHERE id=?
    LIMIT 1;
    `, [id]);

    if (user.rowLength < 1) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);
    if (user.rows[0].get("last_pass_reset") != timestamp || user.rows[0].get("secret") != secret) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    return id;
}