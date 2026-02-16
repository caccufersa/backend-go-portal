package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

type Envelope struct {
	ID        string          `json:"id"`
	Action    string          `json:"action"`
	Service   string          `json:"service"`
	UserID    int             `json:"user_id,omitempty"`
	UserUUID  string          `json:"user_uuid,omitempty"`
	Username  string          `json:"username,omitempty"`
	ReplyTo   string          `json:"reply_to,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     *ErrorPayload   `json:"error,omitempty"`
	Timestamp int64           `json:"ts"`
}

type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func New(action, service string) Envelope {
	return Envelope{
		ID:        generateID(),
		Action:    action,
		Service:   service,
		Timestamp: time.Now().UnixMilli(),
	}
}

func NewRequest(action, service string, data interface{}) (Envelope, error) {
	e := New(action, service)
	raw, err := json.Marshal(data)
	if err != nil {
		return e, err
	}
	e.Data = raw
	return e, nil
}

func NewReply(original Envelope, data interface{}) (Envelope, error) {
	e := New(original.Action+".result", original.Service)
	e.ReplyTo = original.ID
	e.UserID = original.UserID
	e.UserUUID = original.UserUUID
	e.Username = original.Username
	raw, err := json.Marshal(data)
	if err != nil {
		return e, err
	}
	e.Data = raw
	return e, nil
}

func NewEvent(action, service string, data interface{}) (Envelope, error) {
	e := New(action, service)
	raw, err := json.Marshal(data)
	if err != nil {
		return e, err
	}
	e.Data = raw
	return e, nil
}

func NewError(original Envelope, code int, message string) Envelope {
	e := New(original.Action+".error", original.Service)
	e.ReplyTo = original.ID
	e.UserID = original.UserID
	e.UserUUID = original.UserUUID
	e.Error = &ErrorPayload{Code: code, Message: message}
	return e
}

func (e Envelope) Marshal() ([]byte, error) {
	return json.Marshal(e)
}

func Unmarshal(data []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(data, &e)
	return e, err
}

func ParseData[T any](e Envelope) (T, error) {
	var v T
	err := json.Unmarshal(e.Data, &v)
	return v, err
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
