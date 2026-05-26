package redis

// record and get request statistics in admin dashboard

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizxfrog/oh-my-api/internal/db"
)

const (
	requestKeyPrefix = "lingma2api:request:daily:"
	requestKeyTotal  = "lingma2api:request:total"
	requestKeyTTL    = 90 * 24 * time.Hour
)

// RequestStats tracks request count, success count, and TTFT sum in Redis.
type RequestStats struct {
	client *redis.Client
}

// NewRequestStats creates a RequestStats backed by the given Redis client.
func NewRequestStats(client *redis.Client) *RequestStats {
	return &RequestStats{client: client}
}

// RecordRequest increments the daily and total counters for a completed request.
func (rs *RequestStats) RecordRequest(ctx context.Context, success bool, ttftMs int) error {
	todayKey := dailyRequestKey(time.Now())
	successes := 0
	if success {
		successes = 1
	}
	pipe := rs.client.Pipeline()
	pipe.HIncrBy(ctx, todayKey, "requests", 1)
	pipe.HIncrBy(ctx, todayKey, "successes", int64(successes))
	pipe.HIncrBy(ctx, todayKey, "ttft_sum", int64(ttftMs))
	pipe.Expire(ctx, todayKey, requestKeyTTL)
	pipe.HIncrBy(ctx, requestKeyTotal, "requests", 1)
	pipe.HIncrBy(ctx, requestKeyTotal, "successes", int64(successes))
	pipe.HIncrBy(ctx, requestKeyTotal, "ttft_sum", int64(ttftMs))
	_, err := pipe.Exec(ctx)
	return err
}

// GetDashboardStats returns aggregated DashboardStats for the given number of days.
func (rs *RequestStats) GetDashboardStats(ctx context.Context, days int) (db.DashboardStats, error) {
	if days <= 0 {
		days = 1
	}
	now := time.Now()
	var totalRequests, totalSuccesses, ttftSum int64

	for d := 0; d < days; d++ {
		key := dailyRequestKey(now.AddDate(0, 0, -d))
		vals, err := rs.client.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		if n, err := strconv.ParseInt(vals["requests"], 10, 64); err == nil {
			totalRequests += n
		}
		if n, err := strconv.ParseInt(vals["successes"], 10, 64); err == nil {
			totalSuccesses += n
		}
		if n, err := strconv.ParseInt(vals["ttft_sum"], 10, 64); err == nil {
			ttftSum += n
		}
	}

	stats := db.DashboardStats{}
	if totalRequests > 0 {
		stats.TotalRequests = float64(totalRequests)
		stats.SuccessRate = float64(totalSuccesses) * 100.0 / float64(totalRequests)
		stats.AvgTTFTMs = int(ttftSum / totalRequests)
	}
	return stats, nil
}

func dailyRequestKey(t time.Time) string {
	return requestKeyPrefix + t.Format("2006-01-02")
}
