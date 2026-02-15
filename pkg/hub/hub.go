package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

type clientConn struct {
	conn   *websocket.Conn
	userID int
	uuid   string
}

type serviceConn struct {
	conn     *websocket.Conn
	identity ServiceIdentity
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[*websocket.Conn]*clientConn
	services map[*websocket.Conn]*serviceConn
}

func New() *Hub {
	return &Hub{
		clients:  make(map[*websocket.Conn]*clientConn),
		services: make(map[*websocket.Conn]*serviceConn),
	}
}

// HandleServiceConn — um microservice conecta aqui.
// Primeiro msg deve ser ServiceIdentity (JSON).
func (h *Hub) HandleServiceConn(c *websocket.Conn) {
	_, raw, err := c.ReadMessage()
	if err != nil {
		c.Close()
		return
	}

	var ident ServiceIdentity
	if err := json.Unmarshal(raw, &ident); err != nil || ident.Name == "" {
		c.Close()
		return
	}

	sc := &serviceConn{conn: c, identity: ident}
	h.mu.Lock()
	h.services[c] = sc
	h.mu.Unlock()

	log.Printf("[hub] service '%s' conectado (canais: %v)", ident.Name, ident.Channels)

	defer func() {
		h.mu.Lock()
		delete(h.services, c)
		h.mu.Unlock()
		c.Close()
		log.Printf("[hub] service '%s' desconectado", ident.Name)
	}()

	// lê mensagens do service e faz broadcast
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.SentAt.IsZero() {
			msg.SentAt = time.Now()
		}
		h.Broadcast(msg)
	}
}

// HandleClientConn — um cliente frontend conecta aqui (já autenticado).
func (h *Hub) HandleClientConn(c *websocket.Conn, userID int, uuid string) {
	cc := &clientConn{conn: c, userID: userID, uuid: uuid}

	h.mu.Lock()
	h.clients[c] = cc
	h.mu.Unlock()

	log.Printf("[hub] cliente conectado (user_id=%d, uuid=%s)", userID, uuid)

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		c.Close()
		log.Printf("[hub] cliente desconectado (user_id=%d)", userID)
	}()

	// lê mensagens do cliente (ping/pong, ações futuras)
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		// ignora ping silencioso
		if msg.Type == "ping" {
			data, _ := json.Marshal(WSMessage{Type: "pong", SentAt: time.Now()})
			c.WriteMessage(websocket.TextMessage, data)
			continue
		}
		// clientes podem enviar mensagens que são rebroadcast
		msg.UserID = userID
		msg.UserUUID = uuid
		if msg.SentAt.IsZero() {
			msg.SentAt = time.Now()
		}
		h.Broadcast(msg)
	}
}

// Broadcast envia a mensagem para todos os clientes e services relevantes.
func (h *Hub) Broadcast(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, cc := range h.clients {
		cc.conn.WriteMessage(websocket.TextMessage, data)
	}
	for _, sc := range h.services {
		if matchChannel(sc.identity.Channels, msg.Channel) {
			sc.conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

// ClientCount retorna (frontend_clients, services).
func (h *Hub) ClientCount() (int, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients), len(h.services)
}

func matchChannel(subscribed []string, channel string) bool {
	for _, ch := range subscribed {
		if ch == "*" || ch == channel {
			return true
		}
	}
	return false
}
