package db

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type DashboardData struct {
	Stats             DashboardStats    `json:"stats"`
	SuccessRateSeries []TimeSeriesPoint `json:"success_rate_series"`
	TokenSeries       []TimeSeriesPoint `json:"token_series"`
	ModelDistribution []ModelDistPoint  `json:"model_distribution"`
}

type DashboardStats struct {
	TotalRequests float64 `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	TotalTokens   int     `json:"total_tokens"`
	AvgTTFTMs     int     `json:"avg_ttft_ms"`
}

type TimeSeriesPoint struct {
	Time       time.Time `json:"time"`
	Rate       float64   `json:"rate,omitempty"`
	Prompt     int       `json:"prompt,omitempty"`
	Completion int       `json:"completion,omitempty"`
}

type ModelDistPoint struct {
	Model string `json:"model"`
	Count int    `json:"count"`
}

func RangeToHours(rangeStr string) int {
	switch rangeStr {
	case "1h":
		return 1
	case "6h":
		return 6
	case "24h":
		return 24
	case "7d":
		return 24 * 7
	case "30d":
		return 24 * 30
	default:
		return 24
	}
}

func granularityForRange(hours int) string {
	switch {
	case hours <= 1:
		return timeFmtMinute
	case hours <= 24:
		return timeFmtHour
	default:
		return timeFmtDay
	}
}

const (
	timeFmtMinute string = "minute"
	timeFmtHour   string = "hour"
	timeFmtDay    string = "day"
)

// timeGroupExpr returns the SQL expression to truncate a timestamp to the given granularity.
func (s *Store) timeGroupExpr(gran, col string) string {
	switch s.driver {
	case "postgres":
		switch gran {
		case timeFmtMinute:
			return fmt.Sprintf("date_trunc('minute', %s)", col)
		case timeFmtHour:
			return fmt.Sprintf("date_trunc('hour', %s)", col)
		default:
			return fmt.Sprintf("date_trunc('day', %s)", col)
		}
	default: // sqlite
		switch gran {
		case timeFmtMinute:
			return fmt.Sprintf("strftime('%%Y-%%m-%%dT%%H:%%M', %s)", col)
		case timeFmtHour:
			return fmt.Sprintf("strftime('%%Y-%%m-%%dT%%H:00', %s)", col)
		default:
			return fmt.Sprintf("strftime('%%Y-%%m-%%dT00:00', %s)", col)
		}
	}
}

func (s *Store) GetDashboardData(ctx context.Context, rangeStr string) (DashboardData, error) {
	hours := RangeToHours(rangeStr)
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	gran := granularityForRange(hours)

	data := DashboardData{}

	// Check if canonical records exist
	canonicalRecords, err := s.ListCanonicalExecutionRecords(ctx, 1)
	if err != nil {
		return data, err
	}
	if len(canonicalRecords) > 0 {
		return s.getCanonicalDashboardData(ctx, cutoff, gran)
	}

	return s.getLogDashboardData(ctx, cutoff, gran)
}

func (s *Store) getLogDashboardData(ctx context.Context, cutoff time.Time, gran string) (DashboardData, error) {
	data := DashboardData{}
	totalTokens := 0

	row := s.db.QueryRowContext(ctx, s.sql(
		fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END)*100.0/NULLIF(COUNT(*),0),0),
		 COALESCE(AVG(ttft_ms),0), COALESCE(SUM(total_tokens),0)
		 FROM request_logs WHERE created_at>$1`)), cutoff)
	if err := row.Scan(&data.Stats.TotalRequests, &data.Stats.SuccessRate, &data.Stats.AvgTTFTMs, &totalTokens); err != nil {
		return data, err
	}
	data.Stats.TotalTokens = totalTokens

	groupExpr := s.timeGroupExpr(gran, "created_at")

	// Success rate series
	rows, err := s.db.QueryContext(ctx, s.sql(
		fmt.Sprintf(`SELECT %s as t, COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END)*100.0/NULLIF(COUNT(*),0),0) as r
		 FROM request_logs WHERE created_at>$1 GROUP BY %s ORDER BY t`, groupExpr, groupExpr)), cutoff)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p TimeSeriesPoint
			var t time.Time
			var tStr string
			if s.driver == "postgres" {
				if err := rows.Scan(&t, &p.Rate); err != nil {
					continue
				}
			} else {
				if err := rows.Scan(&tStr, &p.Rate); err != nil {
					continue
				}
				t, _ = parseSQLiteTime(gran, tStr)
			}
			p.Time = t
			data.SuccessRateSeries = append(data.SuccessRateSeries, p)
		}
		rows.Close()
	}
	if data.SuccessRateSeries == nil {
		data.SuccessRateSeries = []TimeSeriesPoint{}
	}

	// Token series
	rows2, err := s.db.QueryContext(ctx, s.sql(
		fmt.Sprintf(`SELECT %s as t, COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		 FROM request_logs WHERE created_at>$1 GROUP BY %s ORDER BY t`, groupExpr, groupExpr)), cutoff)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var p TimeSeriesPoint
			var t time.Time
			var tStr string
			if s.driver == "postgres" {
				if err := rows2.Scan(&t, &p.Prompt, &p.Completion); err != nil {
					continue
				}
			} else {
				if err := rows2.Scan(&tStr, &p.Prompt, &p.Completion); err != nil {
					continue
				}
				t, _ = parseSQLiteTime(gran, tStr)
			}
			p.Time = t
			data.TokenSeries = append(data.TokenSeries, p)
		}
		rows2.Close()
	}
	if data.TokenSeries == nil {
		data.TokenSeries = []TimeSeriesPoint{}
	}

	// Model distribution
	rows3, err := s.db.QueryContext(ctx, s.sql(
		`SELECT mapped_model, COUNT(*) as c FROM request_logs WHERE created_at>$1 GROUP BY mapped_model ORDER BY c DESC LIMIT 10`), cutoff)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var p ModelDistPoint
			if err := rows3.Scan(&p.Model, &p.Count); err != nil {
				continue
			}
			data.ModelDistribution = append(data.ModelDistribution, p)
		}
		rows3.Close()
	}
	if data.ModelDistribution == nil {
		data.ModelDistribution = []ModelDistPoint{}
	}

	return data, nil
}

func parseSQLiteTime(gran, tStr string) (time.Time, error) {
	switch gran {
	case timeFmtMinute:
		return time.Parse("2006-01-02T15:04", tStr)
	case timeFmtHour:
		return time.Parse("2006-01-02T15:04", tStr+":00")
	default:
		return time.Parse("2006-01-02T15:04:05", tStr+":00:00")
	}
}

func (s *Store) getCanonicalDashboardData(ctx context.Context, cutoff time.Time, gran string) (DashboardData, error) {
	// For canonical records with token data in JSON, we still need to iterate.
	// However we use a time-limited query to avoid loading all records.
	records, err := s.ListCanonicalExecutionRecordsSince(ctx, cutoff, 10000)
	if err != nil {
		return DashboardData{}, err
	}

	data := DashboardData{
		SuccessRateSeries: []TimeSeriesPoint{},
		TokenSeries:       []TimeSeriesPoint{},
		ModelDistribution: []ModelDistPoint{},
	}

	type rateBucket struct {
		total   int
		success int
	}
	type tokenBucket struct {
		prompt     int
		completion int
	}
	rateBuckets := map[time.Time]*rateBucket{}
	tokenBuckets := map[time.Time]*tokenBucket{}
	modelCounts := map[string]int{}
	var ttftSum int

	for _, record := range records {
		data.Stats.TotalRequests++
		if CanonicalRecordStatus(record) == "success" {
			data.Stats.SuccessRate += 1
		}
		if record.Sidecar != nil {
			ttftSum += record.Sidecar.TTFTMs
		}
		promptTokens, completionTokens, totalTokens := CanonicalRecordTokenCounts(record)
		data.Stats.TotalTokens += totalTokens

		bucketTime := canonicalBucketTime(record.CreatedAt, gran)
		if rateBuckets[bucketTime] == nil {
			rateBuckets[bucketTime] = &rateBucket{}
		}
		rateBuckets[bucketTime].total++
		if CanonicalRecordStatus(record) == "success" {
			rateBuckets[bucketTime].success++
		}
		if tokenBuckets[bucketTime] == nil {
			tokenBuckets[bucketTime] = &tokenBucket{}
		}
		tokenBuckets[bucketTime].prompt += promptTokens
		tokenBuckets[bucketTime].completion += completionTokens
		modelCounts[CanonicalRecordMappedModel(record)]++
	}

	if data.Stats.TotalRequests > 0 {
		data.Stats.SuccessRate = data.Stats.SuccessRate * 100 / data.Stats.TotalRequests
		data.Stats.AvgTTFTMs = ttftSum / int(data.Stats.TotalRequests)
	}
	data.Stats.TotalRequests = float64(len(records))

	for bucketTime, bucket := range rateBuckets {
		data.SuccessRateSeries = append(data.SuccessRateSeries, TimeSeriesPoint{
			Time: bucketTime,
			Rate: float64(bucket.success) * 100 / float64(bucket.total),
		})
	}
	for bucketTime, bucket := range tokenBuckets {
		data.TokenSeries = append(data.TokenSeries, TimeSeriesPoint{
			Time:       bucketTime,
			Prompt:     bucket.prompt,
			Completion: bucket.completion,
		})
	}
	for model, count := range modelCounts {
		data.ModelDistribution = append(data.ModelDistribution, ModelDistPoint{
			Model: model,
			Count: count,
		})
	}
	sort.Slice(data.SuccessRateSeries, func(i, j int) bool {
		return data.SuccessRateSeries[i].Time.Before(data.SuccessRateSeries[j].Time)
	})
	sort.Slice(data.TokenSeries, func(i, j int) bool {
		return data.TokenSeries[i].Time.Before(data.TokenSeries[j].Time)
	})
	sort.Slice(data.ModelDistribution, func(i, j int) bool {
		if data.ModelDistribution[i].Count == data.ModelDistribution[j].Count {
			return data.ModelDistribution[i].Model < data.ModelDistribution[j].Model
		}
		return data.ModelDistribution[i].Count > data.ModelDistribution[j].Count
	})
	if len(data.ModelDistribution) > 10 {
		data.ModelDistribution = data.ModelDistribution[:10]
	}
	return data, nil
}

func (s *Store) ListCanonicalExecutionRecordsSince(ctx context.Context, since time.Time, limit int) ([]CanonicalExecutionRecordRow, error) {
	query := s.sql(`SELECT id,created_at,ingress_protocol,ingress_endpoint,session_id,
		 pre_policy_json,post_policy_json,session_snapshot_json,southbound_request,sidecar_json
		 FROM canonical_execution_records WHERE created_at >= $1 ORDER BY created_at DESC`)
	if limit > 0 {
		rows, err := s.db.QueryContext(ctx, query+s.sql(` LIMIT $2`), since, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanCanonicalRecords(rows)
	}
	rows, err := s.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCanonicalRecords(rows)
}

func scanCanonicalRecords(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}) ([]CanonicalExecutionRecordRow, error) {
	var items []CanonicalExecutionRecordRow
	for rows.Next() {
		record, err := scanCanonicalExecutionRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if items == nil {
		items = []CanonicalExecutionRecordRow{}
	}
	return items, rows.Err()
}

// GetTokenStats is deprecated in favor of Redis-backed token tracking.
// It now returns zeros — callers should use redis.TokenStats instead.
func (s *Store) GetTokenStats(ctx context.Context) (today, week, total int, err error) {
	return 0, 0, 0, nil
}

func canonicalBucketTime(t time.Time, gran string) time.Time {
	t = t.UTC()
	switch gran {
	case timeFmtMinute:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	case timeFmtHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
}
