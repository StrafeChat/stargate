package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/gocql/gocql"
)

var (
	Session *gocql.Session
	Rdb     *redis.Client
)

func InitDB() error {
	hosts := strings.Split(getEnv("SCYLLA_HOSTS", "127.0.0.1"), ",")
	keyspace := getEnv("SCYLLA_KEYSPACE", "strafechatgo")
	username := os.Getenv("SCYLLA_USERNAME")
	password := os.Getenv("SCYLLA_PASSWORD")

	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	
	// Optimize connection settings for better performance
	cluster.Timeout = 2 * time.Second
	cluster.ConnectTimeout = 5 * time.Second
	cluster.Consistency = gocql.LocalQuorum // Better performance than Quorum
	
	// Connection pooling settings for better concurrency
	cluster.NumConns = 4 // Number of connections per host
	cluster.MaxPreparedStmts = 1000 // Cache prepared statements
	cluster.MaxRoutingKeyInfo = 1000 // Cache routing info
	
	// Retry policy for better reliability
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		Min:        100 * time.Millisecond,
		Max:        10 * time.Second,
		NumRetries: 3,
	}
	
	// Connection pooling policy
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())

	if username != "" && password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: username,
			Password: password,
		}
	}

	var err error
	Session, err = cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to create ScyllaDB session: %v", err)
	}
	log.Println("Successfully connected to ScyllaDB")

	// Create keyspace if it doesn't exist
	if err := Session.Query(fmt.Sprintf(`
		CREATE KEYSPACE IF NOT EXISTS %s
		WITH replication = {
			'class': 'SimpleStrategy',
			'replication_factor': 1
		}`, keyspace)).Exec(); err != nil {
		return fmt.Errorf("failed to create keyspace: %v", err)
	}

	redisAddr := getEnv("REDIS_ADDR", os.Getenv("REDIS_HOST"))
	redisPassword := os.Getenv("REDIS_PASSWORD")

	Rdb = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	if err := Rdb.Ping().Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis")

	return nil
}

func CloseDB() {
	if Session != nil {
		Session.Close()
		log.Println("Closed ScyllaDB connection")
	}

	if Rdb != nil {
		if err := Rdb.Close(); err != nil {
			log.Printf("Error closing Redis connection: %v", err)
		} else {
			log.Println("Closed Redis connection")
		}
	}
}

func GetSession() *gocql.Session {
	return Session
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
