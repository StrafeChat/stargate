import { Client } from "cassandra-driver";
import { createClient } from 'redis';
import { SCYLLA_CONTACT_POINTS, SCYLLA_DATA_CENTER, SCYLLA_KEYSPACE, SCYLLA_PASSWORD, SCYLLA_USERNAME } from "../config";

const cassandra = new Client({
    contactPoints: JSON.parse(SCYLLA_CONTACT_POINTS),
    localDataCenter: SCYLLA_DATA_CENTER,
    keyspace: SCYLLA_KEYSPACE,
    credentials: (SCYLLA_USERNAME && SCYLLA_PASSWORD) ? {
        username: SCYLLA_USERNAME,
        password: SCYLLA_PASSWORD
    } : undefined,
});

const redis = createClient();

export const init = async () => {
    await cassandra.connect().catch(console.error);
    await redis.connect().catch(console.error);
}

export { cassandra, redis };
export default { init };