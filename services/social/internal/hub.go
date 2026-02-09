package hub

import (
	"log"
	"sync"

	"cacc/services/social/models"

	"github.com/gofiber/contrib/websocket"
)

type Hub struct {
	clients map[*websocket.Conn]bool
	mu      sync.RWMutex
}

func New() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]bool),
	}
}

func (h *Hub) Register(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Broadcast(msg models.WSMessage) {
	h.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if err := c.WriteJSON(msg); err != nil {
			log.Println("ws write error:", err)
			c.Close()
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
		}
	}
}

func (h *Hub) HandleConnection(c *websocket.Conn) {
	h.Register(c)
	defer func() {
		h.Unregister(c)
		c.Close()
	}()

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
	}
}
