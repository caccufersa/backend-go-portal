package cache

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

type Redis struct {
	client *redis.Client
	ctx    context.Context
}

func New() *Redis {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatal("redis url inválida:", err)
	}

	opt.PoolSize = 10
	opt.MinIdleConns = 3

	client := redis.NewClient(opt)
	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatal("redis ping falhou:", err)
	}

	return &Redis{client: client, ctx: ctx}
}

// Get retrieves JSON-encoded value from cache
func (r *Redis) Get(key string, dest interface{}) bool {
	val, err := r.client.Get(r.ctx, key).Result()
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(val), dest) == nil
}

// Set stores JSON-encoded value in cache
func (r *Redis) Set(key string, value interface{}, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	r.client.Set(r.ctx, key, data, ttl)
}

// GetProto retrieves protobuf-encoded value from cache
func (r *Redis) GetProto(key string, dest proto.Message) bool {
	val, err := r.client.Get(r.ctx, key).Bytes()
	if err != nil {
		return false
	}
	return proto.Unmarshal(val, dest) == nil
}

// SetProto stores protobuf-encoded value in cache (faster + smaller)
func (r *Redis) SetProto(key string, msg proto.Message, ttl time.Duration) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	r.client.Set(r.ctx, key, data, ttl)
}

func (r *Redis) Del(keys ...string) {
	r.client.Del(r.ctx, keys...)
}

// DelPattern deletes keys matching a pattern in batches to look easy on memory
func (r *Redis) DelPattern(pattern string) {
	iter := r.client.Scan(r.ctx, 0, pattern, 0).Iterator()
	const batchSize = 100

	pipe := r.client.Pipeline()
	count := 0

	for iter.Next(r.ctx) {
		pipe.Del(r.ctx, iter.Val())
		count++

		if count >= batchSize {
			pipe.Exec(r.ctx)
			count = 0
		}
	}

	if count > 0 {
		pipe.Exec(r.ctx)
	}
}

func (r *Redis) Close() {
	r.client.Close()
}
