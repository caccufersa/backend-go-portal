package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"cacc/pkg/broker"
	"cacc/pkg/envelope"

	"github.com/gofiber/contrib/websocket"
)

type clientConn struct {
	conn     *websocket.Conn
	userID   int
	uuid     string
	username string
	mu       sync.Mutex
}

func (cc *clientConn) send(data []byte) {
	cc.mu.Lock()
	cc.conn.WriteMessage(websocket.TextMessage, data)
	cc.mu.Unlock()
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*clientConn
	byUser  map[int][]*clientConn
	broker  *broker.Broker
}

func New(b *broker.Broker) *Hub {
	h := &Hub{
		clients: make(map[*websocket.Conn]*clientConn),
		byUser:  make(map[int][]*clientConn),
		broker:  b,
	}

	b.Subscribe("gateway:replies", "gateway:broadcast")

	b.On("", func(env envelope.Envelope) {
		h.deliverToClient(env)
	})

	return h
}

func (h *Hub) HandleClientConn(c *websocket.Conn, userID int, uuid, username string) {
	cc := &clientConn{conn: c, userID: userID, uuid: uuid, username: username}

	h.mu.Lock()
	h.clients[c] = cc
	if userID > 0 {
		h.byUser[userID] = append(h.byUser[userID], cc)
	}
	h.mu.Unlock()

	log.Printf("[gateway] client connected user_id=%d uuid=%s", userID, uuid)

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		if userID > 0 {
			conns := h.byUser[userID]
			for i, conn := range conns {
				if conn == cc {
					h.byUser[userID] = append(conns[:i], conns[i+1:]...)
					break
				}
			}
			if len(h.byUser[userID]) == 0 {
				delete(h.byUser, userID)
			}
		}
		h.mu.Unlock()
		c.Close()
		log.Printf("[gateway] client disconnected user_id=%d", userID)
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}

		var env envelope.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			errEnv := envelope.Envelope{
				Action:    "error",
				Error:     &envelope.ErrorPayload{Code: 400, Message: "JSON inválido"},
				Timestamp: time.Now().UnixMilli(),
			}
			data, _ := errEnv.Marshal()
			cc.send(data)
			continue
		}

		if env.Action == "ping" {
			pong := envelope.New("pong", "gateway")
			data, _ := pong.Marshal()
			cc.send(data)
			continue
		}

		env.UserID = userID
		env.UserUUID = uuid
		env.Username = username
		env.ReplyTo = env.ID

		service := env.Service
		if service == "" {
			errEnv := envelope.NewError(env, 400, "campo 'service' obrigatório")
			data, _ := errEnv.Marshal()
			cc.send(data)
			continue
		}

		channel := "service:" + service
		if err := h.broker.Publish(channel, env); err != nil {
			errEnv := envelope.NewError(env, 502, "serviço indisponível")
			data, _ := errEnv.Marshal()
			cc.send(data)
		}
	}
}

func (h *Hub) deliverToClient(env envelope.Envelope) {
	data, err := env.Marshal()
	if err != nil {
		return
	}

	if env.UserID > 0 {
		h.mu.RLock()
		conns := h.byUser[env.UserID]
		h.mu.RUnlock()
		for _, cc := range conns {
			cc.send(data)
		}
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, cc := range h.clients {
		cc.send(data)
	}
}

func (h *Hub) BroadcastToAll(env envelope.Envelope) {
	data, err := env.Marshal()
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, cc := range h.clients {
		cc.send(data)
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
