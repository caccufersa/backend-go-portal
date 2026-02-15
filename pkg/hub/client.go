package hub

import (
	"encoding/json"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
)

type HubClient struct {
	hubURL    string // ex: "ws://localhost:8082/ws/hub"
	identity  ServiceIdentity
	conn      *websocket.Conn
	mu        sync.Mutex
	done      chan struct{}
	onMessage func(WSMessage) // callback quando recebe msg do hub
}

// NewClient cria um cliente que se conecta ao hub central.
//   - hubURL: endereço WS do auth hub, ex: "ws://localhost:8082/ws/hub"
//   - name: nome deste serviço ("social", "sugestoes", "noticias")
//   - channels: canais que quer escutar (["*"] para todos)
func NewClient(hubURL, name string, channels []string) *HubClient {
	return &HubClient{
		hubURL:   hubURL,
		identity: ServiceIdentity{Name: name, Channels: channels},
		done:     make(chan struct{}),
	}
}

// OnMessage registra callback para mensagens recebidas do hub.
func (c *HubClient) OnMessage(fn func(WSMessage)) {
	c.onMessage = fn
}

// Connect inicia a conexão e mantém-na viva com auto-reconnect.
// Bloqueia em background (chame com go).
func (c *HubClient) Connect() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		if err := c.dial(); err != nil {
			log.Printf("[hub-client:%s] erro conexão: %v — retry em 3s", c.identity.Name, err)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("[hub-client:%s] conectado ao hub %s", c.identity.Name, c.hubURL)
		c.readLoop()
		log.Printf("[hub-client:%s] desconectado — reconectando...", c.identity.Name)
		time.Sleep(1 * time.Second)
	}
}

func (c *HubClient) dial() error {
	u, err := url.Parse(c.hubURL)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	
	identBytes, _ := json.Marshal(c.identity)
	if err := conn.WriteMessage(websocket.TextMessage, identBytes); err != nil {
		conn.Close()
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	return nil
}

func (c *HubClient) readLoop() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if c.onMessage != nil {
			var msg WSMessage
			if err := json.Unmarshal(raw, &msg); err == nil {
				c.onMessage(msg)
			}
		}
	}
}

// Send envia uma mensagem pelo WebSocket para o hub central.
func (c *HubClient) Send(msg WSMessage) error {
	if msg.Service == "" {
		msg.Service = c.identity.Name
	}
	if msg.Channel == "" {
		msg.Channel = c.identity.Name
	}
	if msg.SentAt.IsZero() {
		msg.SentAt = time.Now()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil // será reenviado quando reconectar (ou descartado — fire & forget)
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// Broadcast é um atalho para Send.
func (c *HubClient) Broadcast(msgType string, channel string, data interface{}) {
	if err := c.Send(WSMessage{
		Type:    msgType,
		Channel: channel,
		Data:    data,
	}); err != nil {
		log.Printf("[hub-client:%s] erro broadcast: %v", c.identity.Name, err)
	}
}

// Close encerra a conexão.
func (c *HubClient) Close() {
	close(c.done)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}
