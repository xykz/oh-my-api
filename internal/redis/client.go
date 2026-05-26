package redis

import (
	"context"
	"fmt"

	"github.com/rizxfrog/oh-my-api/internal/config"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a Redis client from config and verifies connectivity.
func NewClient(cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}
