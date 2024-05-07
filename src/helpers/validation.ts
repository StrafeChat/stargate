import { types } from "cassandra-driver";
import { ErrorCodes, ErrorMessages, OpCodes } from "../config";
import { cassandra } from "../database";
import { WebSocket } from "../types";

const fetchData = async (id: string): Promise<{user: types.ResultSet, spaces: types.ResultSet}> => {
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
  `, [user.rows[0].get("space_ids") || []]);

    await Promise.all(spacesDb.rows.map(async (space: any) => {
      let rooms = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.rooms
          WHERE id IN ?
      `, [space.get("room_ids") || []]);

      let members = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.space_members
          WHERE space_id = ?
      `, [space.get("id")]);

      await Promise.all(members.rows.map(async (member) => {
        let user = await cassandra.execute(`
          SELECT username, global_name, avatar, flags, presence, discriminator, created_at, id FROM ${cassandra.keyspace}.users
          WHERE id = ?
      `, [member.get("user_id")]);
        member.user = user.rows[0];
        member.user.username = user.rows[0].username ?? "Deleted User";
        member.user.display_name = user.rows[0].global_name ?? member.user.username;
        member.user.discriminator = user.rows[0].discriminator ?? 0;
      }));
      await Promise.all(rooms.rows.map(async (room) => {
        try {
          let messages = await cassandra.execute(`
                SELECT * FROM ${cassandra.keyspace}.messages
                WHERE room_id = ?
            `, [room.get("id")]);


          await Promise.all(messages.rows.map(async (message) => {
            let author = await cassandra.execute(`
                    SELECT * FROM ${cassandra.keyspace}.users
                    WHERE id = ?
                `, [message.get("author_id")]);

            if (message.embeds && message.embeds[0]) {
              message.embeds.forEach((embed: any) => {
                if (embed.timestamp) embed.timestamp = embed.timestamp.getTime();
              })
            }

            message.author = author.rows[0];
            message.created_at = message.created_at.getTime();
            message.author.username = author.rows[0].username ?? "Deleted User";
            message.author.discriminator = author.rows[0].discriminator ?? 0;
            message.author.display_name = author.rows[0].global_name ?? message.author.username;
          }));
          room.messages = messages.rows;
        } catch (error) {
          console.log(error)
          room.messages = [];
        }
      }));

      space.rooms = rooms.rows;
      space.members = members.rows;
    }));

    spaces = spacesDb;

  }
  return { user, spaces };

}

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
  `, [user.rows[0].get("space_ids") || []]);

  await Promise.all(spacesDb.rows.map(async (space: any) => {
      let rooms = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.rooms
          WHERE id IN ?
      `, [space.get("room_ids") || []]);

      let members = await cassandra.execute(`
          SELECT * FROM ${cassandra.keyspace}.space_members
          WHERE space_id = ?
      `, [space.get("id")]);

      await Promise.all(members.rows.map(async (member) => {
        let user = await cassandra.execute(`
          SELECT username, global_name, avatar, flags, presence, discriminator, created_at, id FROM ${cassandra.keyspace}.users
          WHERE id = ?
      `, [member.get("user_id")]);  
      
      member.user = user.rows[0];
      member.user.username = user.rows[0].username ?? "Deleted User";
      member.user.display_name = user.rows[0].global_name ?? member.user.username;
      member.user.discriminator = user.rows[0].discriminator ?? 0;
      }));
      await Promise.all(rooms.rows.map(async (room) => {
         try {
                let messages = await cassandra.execute(`
                SELECT * FROM ${cassandra.keyspace}.messages
                WHERE room_id = ?
            `, [room.get("id")]);
            
             
           await Promise.all(messages.rows.map(async (message) => {
                let author = await cassandra.execute(`
                    SELECT * FROM ${cassandra.keyspace}.users
                    WHERE id = ?
                `, [message.get("author_id")]);

            if (message.embeds && message.embeds[0]) {
                message.embeds.forEach((embed: any) => {
                    if (embed.timestamp) embed.timestamp = embed.timestamp.getTime();
                })
            }

            message.author = author.rows[0];
            message.created_at = message.created_at.getTime();
            message.author.username = author.rows[0].username ?? "Deleted User";
            message.author.discriminator = author.rows[0].discriminator ?? 0;
            message.author.display_name = author.rows[0].global_name ?? message.author.username;
           }));
            room.messages = messages.rows;
            
         } catch (error) {
            console.log(error)
            room.messages = []; 
        }
      }));

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
    client.token = token;
    client.user = {
        id,
        created_at: user.rows[0].get("created_at"),
        presence: user.rows[0].get("presence"),
        username: user.rows[0].get("username"),
        global_name: user.rows[0].get("global_name"),
        display_name: user.rows[0].get("global_name") ?? user.rows[0].get("username"),
        discriminator: user.rows[0].get("discriminator"),
        avatar: user.rows[0].get("avatar"),
        space_ids: user.rows[0].get("space_ids"),     
    }
}

export const dataRevaluation = async (client: WebSocket) => {
  if (!client.verified) return client.close(ErrorCodes.NOT_AUTHENTICATED, ErrorMessages.NOT_AUTHENTICATED);

  const splitToken = client.token.split(".");

  const id = atob(splitToken[0]);
  const timestamp = atob(splitToken[1]);
  const secret = atob(splitToken[2]);

  const { user, spaces } = await fetchData(id);

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
    username: user.rows[0].get("username"),
    global_name: user.rows[0].get("global_name"),
    display_name: user.rows[0].get("global_name") ?? user.rows[0].get("username"),
    discriminator: user.rows[0].get("discriminator"),
    avatar: user.rows[0].get("avatar"),
    space_ids: user.rows[0].get("space_ids"),
  }
}
