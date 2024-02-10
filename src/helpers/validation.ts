import { types } from "cassandra-driver";
import { ErrorCodes, ErrorMessages, OpCodes } from "../config";
import { cassandra } from "../database";
import { WebSocket } from "../types";

export const verifyToken = async (client: WebSocket, token: string) => {
    if (client.verified) return client.close(ErrorCodes.ALREADY_AUTHENTICATED, ErrorMessages.ALREADY_AUTHENTICATED);

    if (typeof token != "string") return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    const splitToken = token.split('.');

    if (splitToken.length < 3) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    const id = atob(splitToken[0]);
    const timestamp = atob(splitToken[1]);

    // TODO: This will be the session id in the future
    const secret = atob(splitToken[2]);

    const user = await cassandra.execute(`
    SELECT last_pass_reset, secret, username, discriminator, global_name, avatar, bot, system, mfa_enabled, banner, accent_color, locale, verified, email, flags, premium_type, public_flags, avatar_decoration, created_at, edited_at, space_ids, presence FROM ${cassandra.keyspace}.users
      WHERE id=?
      LIMIT 1;
    `, [id]);

    let spaces: any;

    if (user.rows[0].get("space_ids") == undefined || user.rows[0].get("space_ids").length > 0) {
    let spacesDb = await cassandra.execute(`
      SELECT * FROM ${cassandra.keyspace}.spaces
      WHERE id IN ?
  `, [user.rows[0].get("space_ids")]);

  await Promise.all(spacesDb.rows.map(async (space: any) => {
      let rooms = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.rooms
          WHERE id IN ?
      `, [space.get("room_ids")]);

      let members = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.space_members
          WHERE space_id = ?
      `, [space.get("id")]);

      await Promise.all(members.rows.map(async (member) => {
        let user = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.users
          WHERE id = ?
      `, [member.get("user_id")]);
         member.user = user.rows[0];
      }))

      space.rooms = rooms.rows;
      space.members = members.rows;
  }));

  spaces = spacesDb;
}

    if (user.rowLength < 1 || user.rowLength > 3) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);
    if (user.rows[0].get("last_pass_reset").getTime() != timestamp || user.rows[0].get("secret") != secret) return client.close(ErrorCodes.INVALID_TOKEN, ErrorMessages.INVALID_TOKEN);

    client.send(JSON.stringify({
        op: OpCodes.DISPATCH, event: "READY", data: {
            user: {
                ...user.rows[0], last_pass_reset: undefined, secret: undefined,
                id
            },
            spaces: spaces.rows ?? null
        }
    }))
    
    client.spaces = spaces.rows ?? null;
    client.verified = true;
    client.user = {
        id,
        created_at: user.rows[0].get("created_at"),
        presence: user.rows[0].get("presence"),
        username: user.rows[0].get("username")
    }
}
