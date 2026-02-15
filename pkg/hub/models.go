package hub

import "time"

// WSMessage é a mensagem padrão trocada entre todos os serviços e clientes.
type WSMessage struct {
	Type     string      `json:"type"`
	Service  string      `json:"service,omitempty"`
	Channel  string      `json:"channel,omitempty"`
	UserID   int         `json:"user_id,omitempty"`
	UserUUID string      `json:"user_uuid,omitempty"`
	Data     interface{} `json:"data,omitempty"`
	SentAt   time.Time   `json:"sent_at,omitempty"`
}

// ServiceIdentity identifica um microservice ao conectar no hub.
type ServiceIdentity struct {
	Name     string   `json:"name"`
	Channels []string `json:"channels"`
}
