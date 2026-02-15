package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

type connInfo struct {
	conn     *websocket.Conn
	service  string   // "" = frontend client
	channels []string // canais subscritos
	userID   int      // > 0 se for cliente autenticado
}

type Hub struct {
	// conexões de clientes frontend
	clients map[*websocket.Conn]*connInfo
	// conexões de microservices
	services map[*websocket.Conn]*connInfo

	broadcast  chan WSMessage
	register   chan *connInfo
	unregister chan *websocket.Conn

	mu sync.RWMutex
}

// New cria e inicia o hub central.
func New() *Hub {
	h := &Hub{
		clients:    make(map[*websocket.Conn]*connInfo),
		services:   make(map[*websocket.Conn]*connInfo),
		broadcast:  make(chan WSMessage, 512),
		register:   make(chan *connInfo, 128),
		unregister: make(chan *websocket.Conn, 128),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {

		case ci := <-h.register:
			h.mu.Lock()
			if ci.service != "" {
				h.services[ci.conn] = ci
				log.Printf("[hub] serviço conectado: %s (canais: %v)", ci.service, ci.channels)
			} else {
				h.clients[ci.conn] = ci
				log.Printf("[hub] cliente conectado (user_id=%d)", ci.userID)
			}
			h.mu.Unlock()

		case conn := <-h.unregister:
			h.mu.Lock()
			if ci, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				log.Printf("[hub] cliente desconectado (user_id=%d)", ci.userID)
				conn.Close()
			}
			if ci, ok := h.services[conn]; ok {
				delete(h.services, conn)
				log.Printf("[hub] serviço desconectado: %s", ci.service)
				conn.Close()
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			if msg.SentAt.IsZero() {
				msg.SentAt = time.Now()
			}
			h.fanOut(msg)
		}
	}
}

// fanOut envia a mensagem para todos os clientes e serviços que subscrevem o canal.
func (h *Hub) fanOut(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Println("[hub] erro marshal:", err)
		return
	}

	h.mu.RLock()
	// coleciona todos os conns alvo
	targets := make([]*websocket.Conn, 0, len(h.clients)+len(h.services))

	// clientes frontend — recebem tudo (ou filtram por channel no front)
	for conn := range h.clients {
		targets = append(targets, conn)
	}

	// serviços — só se subscrevem ao canal
	for conn, ci := range h.services {
		if msg.Channel == "" || containsStr(ci.channels, msg.Channel) || containsStr(ci.channels, "*") {
			targets = append(targets, conn)
		}
	}
	h.mu.RUnlock()

	for _, conn := range targets {
		go func(c *websocket.Conn) {
			if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
				h.unregister <- c
			}
		}(conn)
	}
}

// Broadcast envia uma mensagem a todo o hub.
func (h *Hub) Broadcast(msg WSMessage) {
	h.broadcast <- msg
}


// HandleServiceConn é chamado quando um microservice se conecta em /ws/hub.
// Espera a primeira mensagem ser um JSON ServiceIdentity.
func (h *Hub) HandleServiceConn(c *websocket.Conn) {
	// ler mensagem de identify
	_, raw, err := c.ReadMessage()
	if err != nil {
		log.Println("[hub] erro leitura identify:", err)
		c.Close()
		return
	}

	var ident ServiceIdentity
	if err := json.Unmarshal(raw, &ident); err != nil || ident.Name == "" {
		log.Println("[hub] identify inválido:", string(raw))
		c.Close()
		return
	}

	ci := &connInfo{
		conn:     c,
		service:  ident.Name,
		channels: ident.Channels,
	}
	h.register <- ci

	defer func() { h.unregister <- c }()

	// loop — lê mensagens de broadcast vindas do serviço
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			break
		}
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Service == "" {
			msg.Service = ident.Name
		}
		if msg.Channel == "" {
			msg.Channel = ident.Name
		}
		h.Broadcast(msg)
	}
}

// HandleClientConn é chamado quando um cliente frontend se conecta em /ws.
// O userID vem do JWT validado no middleware.
func (h *Hub) HandleClientConn(c *websocket.Conn, userID int) {
	ci := &connInfo{
		conn:   c,
		userID: userID,
	}
	h.register <- ci

	defer func() { h.unregister <- c }()

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
		// clientes frontend só leem; ignore mensagens (ou implemente chat futuramente)
	}
}

// ClientCount retorna o total de clientes + serviços conectados.
func (h *Hub) ClientCount() (clients int, services int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients), len(h.services)
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
