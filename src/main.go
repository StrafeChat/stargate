package main

import (
	"log"
	"net/http"
	"os"

	"github.com/StrafeChat/stargate/src/database"
	"github.com/StrafeChat/stargate/src/events"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Error loading .env file: %v", err)
	}

	if err := database.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.CloseDB()

	// Start event listener
	go events.EventManager.StartEventListener()

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Error upgrading connection: %v", err)
			return
		}

		// Create WebSocket handler with request for format selection
		handler := events.NewWebSocketHandler(conn, r)

		defer func() {
			handler.Close()
		}()

		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				break
			}

			if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
				if err := handler.HandlePayload(messageType, p); err != nil {
					log.Printf("Error handling payload: %v", err)
				}
			}
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}