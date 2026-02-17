package hub

import (
	"encoding/json"
	"sync"
	"time"

	"cacc/pkg/envelope"

	"github.com/gofiber/contrib/websocket"
)

type ActionHandler func(envelope.Envelope)

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
	mu       sync.RWMutex
	clients  map[*websocket.Conn]*clientConn
	byUser   map[int][]*clientConn
	handlers map[string]ActionHandler
}

func New() *Hub {
	return &Hub{
		clients:  make(map[*websocket.Conn]*clientConn),
		byUser:   make(map[int][]*clientConn),
		handlers: make(map[string]ActionHandler),
	}
}

func (h *Hub) On(action string, fn ActionHandler) {
	h.handlers[action] = fn
}

func (h *Hub) HandleClientConn(c *websocket.Conn, userID int, uuid, username string) {
	cc := &clientConn{conn: c, userID: userID, uuid: uuid, username: username}

	h.mu.Lock()
	h.clients[c] = cc
	if userID > 0 {
		h.byUser[userID] = append(h.byUser[userID], cc)
	}
	h.mu.Unlock()

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

		handler, ok := h.handlers[env.Action]
		if !ok {
			errEnv := envelope.NewError(env, 404, "ação não encontrada: "+env.Action)
			data, _ := errEnv.Marshal()
			cc.send(data)
			continue
		}

		go handler(env)
	}
}

func (h *Hub) Reply(original envelope.Envelope, data interface{}) {
	env, err := envelope.NewReply(original, data)
	if err != nil {
		return
	}
	h.deliverToUser(env)
}

func (h *Hub) ReplyError(original envelope.Envelope, code int, msg string) {
	env := envelope.NewError(original, code, msg)
	h.deliverToUser(env)
}

func (h *Hub) Broadcast(action, service string, data interface{}) {
	env, err := envelope.NewEvent(action, service, data)
	if err != nil {
		return
	}
	raw, err := env.Marshal()
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, cc := range h.clients {
		cc.send(raw)
	}
}

func (h *Hub) deliverToUser(env envelope.Envelope) {
	raw, err := env.Marshal()
	if err != nil {
		return
	}

	if env.UserID > 0 {
		h.mu.RLock()
		conns := h.byUser[env.UserID]
		h.mu.RUnlock()
		for _, cc := range conns {
			cc.send(raw)
		}
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, cc := range h.clients {
		cc.send(raw)
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
