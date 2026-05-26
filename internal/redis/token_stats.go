package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	tokenKeyPrefix = "lingma2api:token:daily:"
	tokenKeyTotal  = "lingma2api:token:total"
	tokenKeyTTL    = 90 * 24 * time.Hour // 90 days retention for daily keys
)

// TokenStats manages token consumption tracking in Redis.
type TokenStats struct {
	client *redis.Client
}

// NewTokenStats creates a TokenStats backed by the given Redis client.
func NewTokenStats(client *redis.Client) *TokenStats {
	return &TokenStats{client: client}
}

// AddTokens increments today's daily counter and the total counter by the given amount.
func (ts *TokenStats) AddTokens(ctx context.Context, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	todayKey := dailyKey(time.Now())
	pipe := ts.client.Pipeline()
	pipe.IncrBy(ctx, todayKey, int64(tokens))
	pipe.Expire(ctx, todayKey, tokenKeyTTL)
	pipe.IncrBy(ctx, tokenKeyTotal, int64(tokens))
	_, err := pipe.Exec(ctx)
	return err
}

// GetTodayTokens returns the token count consumed today.
func (ts *TokenStats) GetTodayTokens(ctx context.Context) (int, error) {
	val, err := ts.client.Get(ctx, dailyKey(time.Now())).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(val)
}

// GetWeekTokens returns the token count consumed from this Monday to today.
func (ts *TokenStats) GetWeekTokens(ctx context.Context) (int, error) {
	now := time.Now()
	monday := weekStart(now)
	total := 0
	for d := monday; !d.After(now); d = d.AddDate(0, 0, 1) {
		val, err := ts.client.Get(ctx, dailyKey(d)).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("parse token value for %s: %w", dailyKey(d), err)
		}
		total += n
	}
	return total, nil
}

// GetTotalTokens returns the all-time token consumption total.
func (ts *TokenStats) GetTotalTokens(ctx context.Context) (int, error) {
	val, err := ts.client.Get(ctx, tokenKeyTotal).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(val)
}

// GetTokenStats returns today, week, and total token counts in one call.
func (ts *TokenStats) GetTokenStats(ctx context.Context) (today, week, total int, err error) {
	today, err = ts.GetTodayTokens(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	week, err = ts.GetWeekTokens(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	total, err = ts.GetTotalTokens(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	return today, week, total, nil
}

// GetTokensForWindow returns the token count for the last N days.
func (ts *TokenStats) GetTokensForWindow(ctx context.Context, days int) (int, error) {
	if days <= 0 {
		days = 1
	}
	now := time.Now()
	total := 0
	for d := 0; d < days; d++ {
		key := dailyKey(now.AddDate(0, 0, -d))
		val, err := ts.client.Get(ctx, key).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("parse token value for %s: %w", key, err)
		}
		total += n
	}
	return total, nil
}

func dailyKey(t time.Time) string {
	return tokenKeyPrefix + t.Format("2006-01-02")
}

// weekStart returns the Monday of the week containing t.
func weekStart(t time.Time) time.Time {
	weekday := t.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	return time.Date(t.Year(), t.Month(), t.Day()-int(weekday)+1, 0, 0, 0, 0, t.Location())
}
