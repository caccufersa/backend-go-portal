package broker

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"cacc/pkg/envelope"

	"github.com/redis/go-redis/v9"
)

type Broker struct {
	rdb      *redis.Client
	ctx      context.Context
	cancel   context.CancelFunc
	pending  sync.Map
	handlers sync.Map
}

type pendingRequest struct {
	ch      chan envelope.Envelope
	expires time.Time
}

type HandlerFunc func(envelope.Envelope)

func New() *Broker {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatal("redis url inválida:", err)
	}

	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithCancel(context.Background())

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis ping falhou:", err)
	}

	b := &Broker{
		rdb:    rdb,
		ctx:    ctx,
		cancel: cancel,
	}

	go b.cleanupPending()
	return b
}

func (b *Broker) Publish(channel string, env envelope.Envelope) error {
	data, err := env.Marshal()
	if err != nil {
		return err
	}
	return b.rdb.Publish(b.ctx, channel, data).Err()
}

func (b *Broker) Subscribe(channels ...string) {
	sub := b.rdb.Subscribe(b.ctx, channels...)
	ch := sub.Channel()

	go func() {
		defer sub.Close()
		for {
			select {
			case <-b.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var env envelope.Envelope
				if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
					continue
				}

				if env.ReplyTo != "" {
					if val, ok := b.pending.LoadAndDelete(env.ReplyTo); ok {
						pr := val.(*pendingRequest)
						select {
						case pr.ch <- env:
						default:
						}
						continue
					}
				}

				if fn, ok := b.handlers.Load(env.Action); ok {
					go fn.(HandlerFunc)(env)
				}
			}
		}
	}()
}

func (b *Broker) On(action string, fn HandlerFunc) {
	b.handlers.Store(action, fn)
}

func (b *Broker) Request(channel string, env envelope.Envelope, timeout time.Duration) (envelope.Envelope, error) {
	pr := &pendingRequest{
		ch:      make(chan envelope.Envelope, 1),
		expires: time.Now().Add(timeout),
	}
	b.pending.Store(env.ID, pr)

	if err := b.Publish(channel, env); err != nil {
		b.pending.Delete(env.ID)
		return envelope.Envelope{}, err
	}

	select {
	case reply := <-pr.ch:
		return reply, nil
	case <-time.After(timeout):
		b.pending.Delete(env.ID)
		return envelope.Envelope{}, context.DeadlineExceeded
	case <-b.ctx.Done():
		return envelope.Envelope{}, b.ctx.Err()
	}
}

func (b *Broker) Reply(channel string, original envelope.Envelope, data interface{}) error {
	env, err := envelope.NewReply(original, data)
	if err != nil {
		return err
	}
	return b.Publish(channel, env)
}

func (b *Broker) ReplyError(channel string, original envelope.Envelope, code int, msg string) error {
	env := envelope.NewError(original, code, msg)
	return b.Publish(channel, env)
}

func (b *Broker) Broadcast(channel string, action, service string, data interface{}) error {
	env, err := envelope.NewEvent(action, service, data)
	if err != nil {
		return err
	}
	return b.Publish(channel, env)
}

func (b *Broker) Close() {
	b.cancel()
	b.rdb.Close()
}

func (b *Broker) cleanupPending() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			b.pending.Range(func(key, value interface{}) bool {
				pr := value.(*pendingRequest)
				if now.After(pr.expires) {
					b.pending.Delete(key)
				}
				return true
			})
		}
	}
}
