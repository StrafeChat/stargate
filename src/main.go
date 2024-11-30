package main

import (
	"log"
	"net/http"
	"os"

	"github.com/StrafeChat/stargate/src/database"
	"github.com/StrafeChat/stargate/src/events"
	"github.com/StrafeChat/stargate/src/format"
	"github.com/StrafeChat/stargate/src/repository"
	"github.com/StrafeChat/stargate/src/services"
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

	if err := database.InitScyllaDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.CloseScyllaDB()


	notificationRepo := repository.NewNotificationRepository(database.GetSession())

	notificationService := services.NewNotificationService(notificationRepo)

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		log.Printf("New WebSocket connection from %s", r.RemoteAddr)

		formatParam := r.URL.Query().Get("format")
		if formatParam == "" {
			formatParam = "json"
		}

		encoder, err := format.GetEncoder(formatParam)
		if err != nil {
			log.Printf("Invalid format %s, defaulting to JSON", formatParam)
			encoder = &format.JSONEncoder{}
		}

		wsHandler := events.NewWebSocketHandler(conn, encoder, notificationService)
		defer wsHandler.Close()

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket error: %v", err)
				}
				break
			}

			if err := wsHandler.HandlePayload(messageType, message); err != nil {
				log.Printf("Error handling payload: %v", err)
				break
			}
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("WebSocket server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}