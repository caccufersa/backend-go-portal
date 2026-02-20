package hub

import (
	"encoding/json"
	"log"
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
	defer cc.mu.Unlock()
	if err := cc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("[HUB] send error user=%d: %v", cc.userID, err)
	}
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[*websocket.Conn]*clientConn
	byUser   map[int][]*clientConn
	handlers map[string]ActionHandler

	// connMap tracks the originating connection for each request ID
	// so replies go to the exact socket that sent the request
	connMu  sync.RWMutex
	connMap map[string]*clientConn
}

func New() *Hub {
	h := &Hub{
		clients:  make(map[*websocket.Conn]*clientConn),
		byUser:   make(map[int][]*clientConn),
		handlers: make(map[string]ActionHandler),
		connMap:  make(map[string]*clientConn),
	}
	go h.cleanupConnMap()
	return h
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

	log.Printf("[HUB] Client connected: user_id=%d username=%s total=%d", userID, username, h.ClientCount())
	h.Broadcast("userCount", "system", map[string]int{
		"count": h.ClientCount(),
	})

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
		log.Printf("[HUB] Client disconnected: user_id=%d username=%s total=%d", userID, username, h.ClientCount())
		h.Broadcast("userCount", "system", map[string]int{
			"count": h.ClientCount(),
		})
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}

		var env envelope.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			errResp := envelope.Envelope{
				Action:    "error",
				Error:     &envelope.ErrorPayload{Code: 400, Message: "JSON inválido"},
				Timestamp: time.Now().UnixMilli(),
			}
			data, _ := errResp.Marshal()
			cc.send(data)
			continue
		}

		if env.Action == "ping" {
			pong := envelope.New("pong", "system")
			data, _ := pong.Marshal()
			cc.send(data)
			continue
		}

		// Store the original request ID for reply routing
		requestID := env.ID

		// Inject user identity from the WS connection (from JWT)
		env.UserID = userID
		env.UserUUID = uuid
		env.Username = username
		env.ReplyTo = requestID

		// Track which connection sent this request
		h.connMu.Lock()
		h.connMap[requestID] = cc
		h.connMu.Unlock()

		handler, ok := h.handlers[env.Action]
		if !ok {
			errResp := envelope.NewError(env, 404, "ação não encontrada: "+env.Action)
			data, _ := errResp.Marshal()
			cc.send(data)
			h.connMu.Lock()
			delete(h.connMap, requestID)
			h.connMu.Unlock()
			continue
		}

		go handler(env)
	}
}

// Reply sends a response to the specific connection that made the request
func (h *Hub) Reply(original envelope.Envelope, data interface{}) {
	env, err := envelope.NewReply(original, data)
	if err != nil {
		log.Printf("[HUB] Reply marshal error: %v", err)
		return
	}

	raw, err := env.Marshal()
	if err != nil {
		return
	}

	// Find the exact connection that sent the request
	h.connMu.RLock()
	cc, ok := h.connMap[original.ReplyTo]
	h.connMu.RUnlock()

	if ok {
		cc.send(raw)
		h.connMu.Lock()
		delete(h.connMap, original.ReplyTo)
		h.connMu.Unlock()
		return
	}

	// Fallback: send to all connections of this user
	if original.UserID > 0 {
		h.mu.RLock()
		conns := h.byUser[original.UserID]
		h.mu.RUnlock()
		for _, c := range conns {
			c.send(raw)
		}
	}
}

// ReplyError sends an error response to the specific connection
func (h *Hub) ReplyError(original envelope.Envelope, code int, msg string) {
	env := envelope.NewError(original, code, msg)
	raw, err := env.Marshal()
	if err != nil {
		return
	}

	h.connMu.RLock()
	cc, ok := h.connMap[original.ReplyTo]
	h.connMu.RUnlock()

	if ok {
		cc.send(raw)
		h.connMu.Lock()
		delete(h.connMap, original.ReplyTo)
		h.connMu.Unlock()
		return
	}

	if original.UserID > 0 {
		h.mu.RLock()
		conns := h.byUser[original.UserID]
		h.mu.RUnlock()
		for _, c := range conns {
			c.send(raw)
		}
	}
}

// Broadcast sends an event to ALL connected clients
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

// BroadcastExcept sends to all clients except the given user
func (h *Hub) BroadcastExcept(action, service string, data interface{}, exceptUserID int) {
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
		if cc.userID != exceptUserID {
			cc.send(raw)
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) AuthenticatedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser)
}

// cleanupConnMap removes stale request tracking entries periodically
func (h *Hub) cleanupConnMap() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.connMu.Lock()
		if len(h.connMap) > 10000 {
			h.connMap = make(map[string]*clientConn)
		}
		h.connMu.Unlock()
	}
}
