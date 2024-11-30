package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

var Session *gocql.Session

func InitScyllaDB() error {
	hosts := strings.Split(getEnv("SCYLLA_HOSTS", "127.0.0.1"), ",")
	keyspace := getEnv("SCYLLA_KEYSPACE", "strafechatgo")
	username := os.Getenv("SCYLLA_USERNAME")
	password := os.Getenv("SCYLLA_PASSWORD")

	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Timeout = 5 * time.Second
	cluster.ConnectTimeout = 5 * time.Second
	cluster.Consistency = gocql.Quorum

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
	return nil
}

func CloseScyllaDB() {
	if Session != nil {
		Session.Close()
		log.Println("Closed ScyllaDB connection")
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
