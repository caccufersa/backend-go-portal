package hub

import "time"

type WSMessage struct {
	Type    string      `json:"type"`              // tipo do evento: "new_post", "new_sugestao", etc.
	Service string      `json:"service,omitempty"` // serviço de origem
	Channel string      `json:"channel,omitempty"` // canal/tópico: "social", "sugestoes", "noticias", etc.
	UserID  int         `json:"user_id,omitempty"` // user associado (se houver)
	Data    interface{} `json:"data"`              // payload
	SentAt  time.Time   `json:"sent_at"`           // timestamp
}

type ServiceIdentity struct {
	Name     string   `json:"name"`     // "social", "noticias", "sugestoes"
	Channels []string `json:"channels"` // canais que o serviço quer escutar
}
