require("dotenv").config();
export const PORT = process.env.PORT ? parseInt(process.env.PORT) : 8080;

const {
    SCYLLA_CONTACT_POINTS,
    SCYLLA_DATA_CENTER,
    SCYLLA_USERNAME,
    SCYLLA_PASSWORD,
    SCYLLA_KEYSPACE,
} = process.env as Record<string, string>;

if (!SCYLLA_CONTACT_POINTS) throw new Error('Missing an array of contact points for Cassandra or Scylla in the environmental variables.');
if (!SCYLLA_DATA_CENTER) throw new Error('Missing data center for Cassandra or Scylla in the environmental variables.');
if (!SCYLLA_KEYSPACE) throw new Error('Missing keyspace for Cassandra or Scylla in the environmental variables.');

export { SCYLLA_CONTACT_POINTS, SCYLLA_DATA_CENTER, SCYLLA_USERNAME, SCYLLA_PASSWORD, SCYLLA_KEYSPACE };

export enum OpCodes {
    IDENTIFY = 0,
    HEARTBEAT = 2,
}

export enum ErrorCodes {
    UNKNOWN = 4000,
    UNKNOWN_OPCODE = 4001,
    DECODE_ERROR = 4002,
    NOT_AUTHENTICATED = 4003,
    INVALID_TOKEN = 4004,
    ALREADY_AUTHENTICATED = 4005,
    SESSION_TIMED_OUT = 4006,
}

export enum ErrorMessages {
    INVALID_TOKEN = "The token in your identify payload was incorrect.",
    DECODE_ERROR = "You sent an invalid payload, you should not do that!",
    SESSION_TIMED_OUT = "Your session has timed out, reconnect to start a new one.",
    NOT_AUTHENTICATED = "You need to authenticate before you can do this!",
}

export const HEARTBEAT = process.env.HEARTBEAT ? parseInt(process.env.HEARTBEAT) : 5000;