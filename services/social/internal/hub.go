package hub

import (
	"sync"

	"cacc/services/social/models"

	"github.com/gofiber/contrib/websocket"
)

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan models.WSMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

func New() *Hub {
	h := &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan models.WSMessage, 256), // buffer maior
		register:   make(chan *websocket.Conn, 64),
		unregister: make(chan *websocket.Conn, 64),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*websocket.Conn, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			for _, client := range clients {
				go func(c *websocket.Conn) {
					if err := c.WriteJSON(message); err != nil {
						h.unregister <- c
					}
				}(client)
			}
		}
	}
}

func (h *Hub) HandleConnection(c *websocket.Conn) {
	h.register <- c
	defer func() {
		h.unregister <- c
	}()

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *Hub) Broadcast(msg models.WSMessage) {
	h.broadcast <- msg
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
