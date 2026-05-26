package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/config"
	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/policy"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

const overviewRecentRequestLimit = 8
const overviewLatencySampleLimit = 120

type adminOverviewResponse struct {
	Healthy         bool                   `json:"healthy"`
	GeneratedAt     time.Time              `json:"generated_at"`
	Credential      proxy.CredentialStatus `json:"credential"`
	Models          proxy.ModelStatus      `json:"models"`
	SessionCount    int                    `json:"session_count"`
	TokenStats      map[string]int         `json:"token_stats"`
	Dashboard       db.DashboardData       `json:"dashboard"`
	Latency         adminLatencyStats      `json:"latency"`
	RecentRequests  []db.RequestLog        `json:"recent_requests"`
	AvailableModels []proxy.OpenAIModel    `json:"available_models"`
	Settings        map[string]string      `json:"settings"`
}

type adminLatencyStats struct {
	AvgMs       int `json:"avg_ms"`
	P50Ms       int `json:"p50_ms"`
	P95Ms       int `json:"p95_ms"`
	MaxMs       int `json:"max_ms"`
	SampleCount int `json:"sample_count"`
}

type adminModelsResponse struct {
	Items  []proxy.OpenAIModel `json:"items"`
	Status proxy.ModelStatus   `json:"status"`
}

type adminAccountPoolResponse struct {
	RoutingMode string                 `json:"routing_mode"`
	LoadBalance string                 `json:"load_balance"`
	Counts      map[string]int         `json:"counts"`
	Accounts    []proxy.AccountSummary `json:"accounts"`
	TokenStats  map[string]int         `json:"token_stats"`
	Status      proxy.CredentialStatus `json:"status"`
	StoredMeta  proxy.StoredMetaInfo   `json:"stored_meta"`
	OAuth       map[string]bool        `json:"oauth"`
	Credential  adminSafeCredential    `json:"credential"`
}

type adminAccountRefreshPoolResponse struct {
	RoutingMode string                 `json:"routing_mode"`
	LoadBalance string                 `json:"load_balance"`
	Counts      map[string]int         `json:"counts"`
	Accounts    []proxy.AccountSummary `json:"accounts"`
	Status      proxy.CredentialStatus `json:"status"`
	StoredMeta  proxy.StoredMetaInfo   `json:"stored_meta"`
	OAuth       map[string]bool        `json:"oauth"`
}

type adminSafeCredential struct {
	CosyKey         string    `json:"cosy_key"`
	EncryptUserInfo string    `json:"encrypt_user_info"`
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	UserID          string    `json:"user_id"`
	MachineID       string    `json:"machine_id"`
	LoadedAt        time.Time `json:"loaded_at"`
}

type accountRefreshProvider interface {
	Refresh(context.Context) ([]proxy.AccountSnapshot, error)
}

func (s *Server) HandleAdminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	overview, err := s.buildAdminOverview(r)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, overview)
}

func (s *Server) HandleAdminModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, "GET, POST")
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if r.Method == http.MethodPost {
		if err := s.Deps.Models.Refresh(r.Context()); err != nil {
			response.WriteMappedError(w, err)
			return
		}
	}

	models, err := s.Deps.Models.ListModels(r.Context())
	if err != nil {
		response.WriteMappedError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, adminModelsResponse{
		Items:  models,
		Status: s.Deps.Models.Status(),
	})
}

func (s *Server) HandleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rangeParam := r.URL.Query().Get("range")
	if rangeParam == "" {
		rangeParam = "24h"
	}
	data, err := s.DB.GetDashboardData(r.Context(), rangeParam)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}

func (s *Server) buildAdminOverview(r *http.Request) (adminOverviewResponse, error) {
	overview := adminOverviewResponse{
		GeneratedAt: s.Deps.Now(),
		Credential:  s.adminCredentialStatus(r.Context()),
		Models:      s.Deps.Models.Status(),
		TokenStats: map[string]int{
			"today": 0,
			"week":  0,
			"total": 0,
		},
		Dashboard: db.DashboardData{
			SuccessRateSeries: []db.TimeSeriesPoint{},
			TokenSeries:       []db.TimeSeriesPoint{},
			ModelDistribution: []db.ModelDistPoint{},
		},
		RecentRequests:  []db.RequestLog{},
		AvailableModels: []proxy.OpenAIModel{},
		Settings:        map[string]string{},
	}

	if sessions, err := s.Deps.Sessions.List(r.Context()); err == nil {
		overview.SessionCount = len(sessions)
	}

	if s.DB != nil {
		if settings, err := s.DB.GetSettings(r.Context()); err == nil && settings != nil {
			overview.Settings = settings
		}
	}
	if s.TokenStats != nil {
		if today, week, total, err := s.TokenStats.GetTokenStats(r.Context()); err == nil {
			overview.TokenStats["today"] = today
			overview.TokenStats["week"] = week
			overview.TokenStats["total"] = total
		}
	}
	if s.DB != nil {
		if dashboard, err := s.DB.GetDashboardData(r.Context(), "24h"); err == nil {
			overview.Dashboard = dashboard
		}
		recent, err := s.listAdminLogs(r.Context(), db.LogFilter{}, 1, overviewRecentRequestLimit)
		if err == nil {
			overview.RecentRequests = recent.Items
		}
		latencySample, err := s.exportAdminLogs(r.Context(), db.LogFilter{})
		if err == nil {
			if len(latencySample) > overviewLatencySampleLimit {
				latencySample = latencySample[:overviewLatencySampleLimit]
			}
			overview.Latency = buildLatencyStats(latencySample)
		}
	}

	models, modelErr := s.Deps.Models.ListModels(r.Context())
	if modelErr == nil {
		overview.AvailableModels = models
		overview.Models = s.Deps.Models.Status()
	}

	overview.Healthy = overview.Credential.Loaded && overview.Credential.HasCredentials && (overview.Models.Cached || len(overview.AvailableModels) > 0)
	return overview, nil
}

func (s *Server) adminCredentialStatus(ctx context.Context) proxy.CredentialStatus {
	if s.Deps.Credentials != nil {
		return s.Deps.Credentials.Status()
	}
	if s.Deps.Accounts == nil {
		return proxy.CredentialStatus{}
	}
	summaries, err := s.Deps.Accounts.Summaries(ctx)
	if err != nil || len(summaries) == 0 {
		accounts, accountErr := s.Deps.Accounts.Accounts(ctx)
		if accountErr != nil {
			return proxy.CredentialStatus{}
		}
		summaries = accountSummariesFromSnapshots(accounts)
	}
	return credentialStatusFromAccountSummaries(summaries)
}

func credentialStatusFromAccountSummaries(summaries []proxy.AccountSummary) proxy.CredentialStatus {
	status := proxy.CredentialStatus{}
	for _, summary := range summaries {
		hasCredentials := summary.HasCosyKey || summary.HasEncryptInfo || summary.HasAccessToken || summary.HasRefreshToken
		if !hasCredentials {
			continue
		}
		if !summary.Enabled && status.Loaded {
			continue
		}
		status.Loaded = true
		status.HasCredentials = true
		status.Source = summary.Source
		status.LoadedAt = summary.LoadedAt
		status.TokenExpired = summary.TokenExpired
		if summary.Enabled {
			return status
		}
	}
	return status
}

func accountSummariesFromSnapshots(accounts []proxy.AccountSnapshot) []proxy.AccountSummary {
	if len(accounts) == 0 {
		return nil
	}
	summaries := make([]proxy.AccountSummary, len(accounts))
	for i, account := range accounts {
		summaries[i] = account.Summary(5 * time.Minute)
	}
	return summaries
}

func buildLatencyStats(logs []db.RequestLog) adminLatencyStats {
	values := make([]int, 0, len(logs))
	for _, log := range logs {
		if ms := logLatencyMs(log); ms > 0 {
			values = append(values, ms)
		}
	}
	if len(values) == 0 {
		return adminLatencyStats{}
	}
	sort.Ints(values)
	sum := 0
	for _, value := range values {
		sum += value
	}
	return adminLatencyStats{
		AvgMs:       int(math.Round(float64(sum) / float64(len(values)))),
		P50Ms:       percentileValue(values, 0.50),
		P95Ms:       percentileValue(values, 0.95),
		MaxMs:       values[len(values)-1],
		SampleCount: len(values),
	}
}

func percentileValue(sorted []int, p float64) int {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(sorted))*p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func logLatencyMs(log db.RequestLog) int {
	switch {
	case log.UpstreamMs > 0:
		return log.UpstreamMs
	case log.DownstreamMs > 0:
		return log.DownstreamMs
	case log.TTFTMs > 0:
		return log.TTFTMs
	default:
		return 0
	}
}

func (s *Server) HandleAdminAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.Deps.Accounts != nil {
		summaries, err := s.Deps.Accounts.Summaries(r.Context())
		if err != nil {
			if isEmptyAccountPoolError(err) {
				summaries = []proxy.AccountSummary{}
			} else {
				response.WriteMappedError(w, err)
				return
			}
		}
		today, week, total := 0, 0, 0
		if s.TokenStats != nil {
			today, week, total, _ = s.TokenStats.GetTokenStats(r.Context())
		}
		status := proxy.CredentialStatus{}
		storedMeta := proxy.StoredMetaInfo{}
		if s.Deps.Credentials != nil {
			status = s.Deps.Credentials.Status()
			storedMeta = s.Deps.Credentials.StoredMeta()
		}
		response.WriteJSON(w, http.StatusOK, adminAccountPoolResponse{
			RoutingMode: s.Deps.AccountConfig.RoutingMode,
			LoadBalance: s.Deps.AccountConfig.LoadBalance,
			Counts:      accountSummaryCounts(summaries),
			Accounts:    summaries,
			TokenStats: map[string]int{
				"today": today,
				"week":  week,
				"total": total,
			},
			Status:     status,
			StoredMeta: storedMeta,
			OAuth:      accountSummaryOAuth(summaries),
			Credential: safeCredentialFromSummaries(summaries),
		})
		return
	}
	if s.Deps.Credentials == nil {
		response.WriteJSON(w, http.StatusOK, map[string]any{
			"credential": proxy.CredentialSnapshot{},
			"status":     proxy.CredentialStatus{},
			"token_stats": map[string]int{
				"today": 0,
				"week":  0,
				"total": 0,
			},
			"stored_meta": proxy.StoredMetaInfo{},
			"oauth": map[string]bool{
				"has_access_token":  false,
				"has_refresh_token": false,
			},
		})
		return
	}
	cred, _ := s.Deps.Credentials.Current(r.Context())
	today, week, total := 0, 0, 0
	if s.TokenStats != nil {
		today, week, total, _ = s.TokenStats.GetTokenStats(r.Context())
	}
	storedMeta := s.Deps.Credentials.StoredMeta()
	hasAT, hasRT := s.Deps.Credentials.HasOAuth()
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"credential": cred,
		"status":     s.Deps.Credentials.Status(),
		"token_stats": map[string]int{
			"today": today,
			"week":  week,
			"total": total,
		},
		"stored_meta": storedMeta,
		"oauth": map[string]bool{
			"has_access_token":  hasAT,
			"has_refresh_token": hasRT,
		},
	})
}

func accountSummaryCounts(accounts []proxy.AccountSummary) map[string]int {
	counts := map[string]int{
		"total":         len(accounts),
		"enabled":       0,
		"china":         0,
		"international": 0,
	}
	for _, account := range accounts {
		if account.Enabled {
			counts["enabled"]++
		}
		switch account.Region {
		case proxy.AccountRegionChina:
			counts["china"]++
		case proxy.AccountRegionInternational:
			counts["international"]++
		}
	}
	return counts
}

func accountSummaryOAuth(accounts []proxy.AccountSummary) map[string]bool {
	oauth := map[string]bool{
		"has_access_token":  false,
		"has_refresh_token": false,
	}
	for _, account := range accounts {
		oauth["has_access_token"] = oauth["has_access_token"] || account.HasAccessToken
		oauth["has_refresh_token"] = oauth["has_refresh_token"] || account.HasRefreshToken
	}
	return oauth
}

func safeCredentialFromSummaries(accounts []proxy.AccountSummary) adminSafeCredential {
	for _, account := range accounts {
		if account.Enabled && (account.UserID != "" || account.MachineID != "" || !account.LoadedAt.IsZero()) {
			return adminSafeCredential{
				UserID:    account.UserID,
				MachineID: account.MachineID,
				LoadedAt:  account.LoadedAt,
			}
		}
	}
	for _, account := range accounts {
		if account.UserID != "" || account.MachineID != "" || !account.LoadedAt.IsZero() {
			return adminSafeCredential{
				UserID:    account.UserID,
				MachineID: account.MachineID,
				LoadedAt:  account.LoadedAt,
			}
		}
	}
	return adminSafeCredential{}
}

func (s *Server) HandleAdminAccountTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.Deps.Accounts != nil {
		s.handleAdminAccountPoolTest(w, r)
		return
	}
	if s.Deps.Credentials == nil {
		response.WriteJSON(w, http.StatusOK, map[string]any{
			"success":          false,
			"status_code":      0,
			"response_preview": "",
			"error":            "credentials unavailable",
			"credential_snapshot": map[string]bool{
				"has_cosy_key":          false,
				"has_encrypt_user_info": false,
				"has_user_id":           false,
				"has_machine_id":        false,
			},
			"timestamp": s.Deps.Now().Format(time.RFC3339),
		})
		return
	}

	cred, err := s.Deps.Credentials.Current(r.Context())
	if err != nil {
		log.Printf("[account-test] credential load failed: %v", err)
		response.WriteJSON(w, http.StatusOK, map[string]any{
			"success":          false,
			"status_code":      0,
			"response_preview": "",
			"error":            err.Error(),
			"credential_snapshot": map[string]bool{
				"has_cosy_key":          false,
				"has_encrypt_user_info": false,
				"has_user_id":           false,
				"has_machine_id":        false,
			},
			"timestamp": s.Deps.Now().Format(time.RFC3339),
		})
		return
	}

	snapshot := map[string]bool{
		"has_cosy_key":          cred.CosyKey != "",
		"has_encrypt_user_info": cred.EncryptUserInfo != "",
		"has_user_id":           cred.UserID != "",
		"has_machine_id":        cred.MachineID != "",
	}

	log.Printf("[account-test] credential: user_id=%s machine_id=%s cosy_key_len=%d encrypt_info_len=%d source=%s",
		cred.UserID, cred.MachineID, len(cred.CosyKey), len(cred.EncryptUserInfo), cred.Source)

	models, testErr := s.Deps.Models.ListModels(r.Context())
	if testErr != nil {
		var upstream *proxy.UpstreamHTTPError
		statusCode := 0
		responsePreview := ""
		if errors.As(testErr, &upstream) {
			statusCode = upstream.StatusCode
			responsePreview = upstream.Body
		}
		log.Printf("[account-test] ListModels failed: %v (status=%d)", testErr, statusCode)
		response.WriteJSON(w, http.StatusOK, map[string]any{
			"success":             false,
			"status_code":         statusCode,
			"response_preview":    responsePreview,
			"error":               testErr.Error(),
			"credential_snapshot": snapshot,
			"cosy_key_prefix":     safePrefix(cred.CosyKey, 20),
			"user_id":             cred.UserID,
			"timestamp":           s.Deps.Now().Format(time.RFC3339),
		})
		return
	}

	log.Printf("[account-test] ListModels success: %d models", len(models))
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"status_code":         200,
		"response_preview":    fmt.Sprintf("ListModels returned %d models", len(models)),
		"error":               "",
		"credential_snapshot": snapshot,
		"cosy_key_prefix":     safePrefix(cred.CosyKey, 20),
		"user_id":             cred.UserID,
		"timestamp":           s.Deps.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleAdminAccountPoolTest(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.Deps.Accounts.Accounts(r.Context())
	if err != nil {
		response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(proxy.AccountTestResult{
			Success:   false,
			Error:     err.Error(),
			Timestamp: s.Deps.Now().Format(time.RFC3339),
		}, proxy.AccountSnapshot{}))
		return
	}
	eligible := s.eligibleAdminTestAccounts(accounts)
	if len(eligible) == 0 {
		response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(proxy.AccountTestResult{
			Success:   false,
			Error:     "no eligible accounts",
			Timestamp: s.Deps.Now().Format(time.RFC3339),
		}, proxy.AccountSnapshot{}))
		return
	}

	account := eligible[0]
	if id := r.URL.Query().Get("id"); id != "" {
		found := false
		for _, candidate := range eligible {
			if candidate.ID == id {
				account = candidate
				found = true
				break
			}
		}
		if !found {
			response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(proxy.AccountTestResult{
				Success:   false,
				Error:     fmt.Sprintf("account %q is not eligible or does not exist", id),
				Timestamp: s.Deps.Now().Format(time.RFC3339),
			}, proxy.AccountSnapshot{}))
			return
		}
	}

	if s.Deps.Adapters == nil {
		response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(proxy.AccountTestResult{
			AccountID:    account.ID,
			AccountLabel: account.Label,
			Region:       account.Region,
			Success:      false,
			Error:        "account adapters unavailable",
			Timestamp:    s.Deps.Now().Format(time.RFC3339),
		}, account))
		return
	}
	adapter, err := s.Deps.Adapters.ForRegion(account.Region)
	if err != nil {
		response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(proxy.AccountTestResult{
			AccountID:    account.ID,
			AccountLabel: account.Label,
			Region:       account.Region,
			Success:      false,
			Error:        err.Error(),
			Timestamp:    s.Deps.Now().Format(time.RFC3339),
		}, account))
		return
	}
	response.WriteJSON(w, http.StatusOK, accountTestResultWithCompatibility(adapter.TestConnection(r.Context(), account), account))
}

func (s *Server) eligibleAdminTestAccounts(accounts []proxy.AccountSnapshot) []proxy.AccountSnapshot {
	pool := s.Deps.AccountPool
	if pool == nil {
		pool = proxy.NewAccountPool(config.AccountConfig{RoutingMode: "mixed"})
	}
	return pool.Eligible(accounts)
}

func accountTestResultWithCompatibility(result proxy.AccountTestResult, account proxy.AccountSnapshot) map[string]any {
	return map[string]any{
		"account_id":          result.AccountID,
		"account_label":       result.AccountLabel,
		"region":              result.Region,
		"success":             result.Success,
		"status_code":         result.StatusCode,
		"response_preview":    result.ResponsePreview,
		"error":               result.Error,
		"timestamp":           result.Timestamp,
		"user_id":             account.UserID,
		"cosy_key_prefix":     "",
		"credential_snapshot": safeAccountCredentialSnapshot(account),
	}
}

func safeAccountCredentialSnapshot(account proxy.AccountSnapshot) map[string]any {
	return map[string]any{
		"has_cosy_key":          account.CosyKey != "",
		"has_encrypt_user_info": account.EncryptUserInfo != "",
		"has_user_id":           account.UserID != "",
		"has_machine_id":        account.MachineID != "",
		"user_id":               account.UserID,
		"cosy_key_prefix":       "",
	}
}

func safePrefix(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (s *Server) HandleAdminAccountRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.Deps.Accounts != nil {
		summaries, err := s.refreshAdminAccountSummaries(r.Context())
		if err != nil {
			response.WriteMappedError(w, err)
			return
		}
		storedMeta := proxy.StoredMetaInfo{}
		status := credentialStatusFromAccountSummaries(summaries)
		if s.Deps.Credentials != nil {
			storedMeta = s.Deps.Credentials.StoredMeta()
			status = s.Deps.Credentials.Status()
		}
		response.WriteJSON(w, http.StatusOK, adminAccountRefreshPoolResponse{
			RoutingMode: s.Deps.AccountConfig.RoutingMode,
			LoadBalance: s.Deps.AccountConfig.LoadBalance,
			Counts:      accountSummaryCounts(summaries),
			Accounts:    summaries,
			Status:      status,
			StoredMeta:  storedMeta,
			OAuth:       accountSummaryOAuth(summaries),
		})
		return
	}
	if s.Deps.Credentials == nil {
		response.WriteMappedError(w, proxy.ErrCredentialsUnavailable)
		return
	}
	cred, err := s.Deps.Credentials.Refresh(r.Context())
	if err != nil {
		response.WriteMappedError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"credential": cred})
}

func (s *Server) refreshAdminAccountSummaries(ctx context.Context) ([]proxy.AccountSummary, error) {
	if refresher, ok := s.Deps.Accounts.(accountRefreshProvider); ok {
		accounts, err := refresher.Refresh(ctx)
		if err != nil {
			if isEmptyAccountPoolError(err) {
				return []proxy.AccountSummary{}, nil
			}
			return nil, err
		}
		return accountSummariesFromSnapshots(accounts), nil
	}
	accounts, err := s.Deps.Accounts.Accounts(ctx)
	if err == nil {
		return accountSummariesFromSnapshots(accounts), nil
	}
	if isEmptyAccountPoolError(err) {
		return []proxy.AccountSummary{}, nil
	}
	summaries, summaryErr := s.Deps.Accounts.Summaries(ctx)
	if summaryErr != nil {
		return nil, err
	}
	return summaries, nil
}

func isEmptyAccountPoolError(err error) bool {
	if !errors.Is(err, proxy.ErrCredentialsUnavailable) {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "no accounts") {
		return true
	}
	if strings.Contains(message, "parse auth file: unexpected end of json input") {
		return true
	}
	return strings.Contains(message, "read auth file:") &&
		(strings.Contains(message, "no such file") || strings.Contains(message, "cannot find the file"))
}

func (s *Server) HandleAdminSettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	settings, err := s.DB.GetSettings(r.Context())
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, settings)
}

func (s *Server) HandleAdminSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.WriteMethodNotAllowed(w, http.MethodPut)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.DB.UpdateSettings(r.Context(), settings); err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleAdminLogsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}

	filter := db.LogFilter{
		Status: q.Get("status"),
		Model:  q.Get("model"),
	}
	if from := q.Get("from"); from != "" {
		filter.From, _ = parseTime(from)
	}
	if to := q.Get("to"); to != "" {
		filter.To, _ = parseTime(to)
	}

	result, err := s.listAdminLogs(r.Context(), filter, page, limit)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) HandleAdminLogsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/logs/")
	id = strings.TrimSuffix(id, "/replay")
	if id == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "missing log id")
		return
	}
	log, _, err := s.getAdminLog(r.Context(), id)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusNotFound, "log not found")
		return
	}
	response.WriteJSON(w, http.StatusOK, log)
}

func (s *Server) HandleAdminLogsReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/logs/")
	id = strings.TrimSuffix(id, "/replay")
	bodyBytes, _ := io.ReadAll(r.Body)
	var replayBody io.ReadCloser
	replayPath := "/v1/chat/completions"
	if len(bodyBytes) > 0 {
		replayBody = io.NopCloser(bytes.NewReader(bodyBytes))
	} else {
		record, err := s.DB.GetCanonicalExecutionRecord(r.Context(), id)
		if err == nil {
			canonicalRequest := canonicalReplayRequestForMode(record, r.URL.Query().Get("mode"))
			marshaled, marshalErr := marshalReplayBodyFromCanonical(canonicalRequest)
			if marshalErr != nil {
				response.WriteOpenAIError(w, http.StatusInternalServerError, marshalErr.Error())
				return
			}
			replayBody = io.NopCloser(bytes.NewReader(marshaled))
			replayPath = replayEndpointForCanonicalRequest(canonicalRequest, record.IngressEndpoint)
			if isHistoricalReplayMode(r.URL.Query().Get("mode")) {
				r.Header.Set("X-Replay-Mode", "historical")
			}
		} else {
			replayReq, getErr := s.DB.GetLog(r.Context(), id)
			if getErr != nil {
				response.WriteOpenAIError(w, http.StatusNotFound, "log not found")
				return
			}
			replayBody = io.NopCloser(strings.NewReader(replayReq.DownstreamReq))
		}
	}
	newReq := r.Clone(r.Context())
	newReq.Method = http.MethodPost
	newReq.URL.Path = replayPath
	newReq.Body = replayBody
	if replayPath == "/v1/messages" {
		s.HandleAnthropicMessages(w, newReq)
		return
	}
	s.HandleChatCompletions(w, newReq)
}

func (s *Server) HandleAdminLogsCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	settings, _ := s.DB.GetSettings(r.Context())
	days := 30
	if d, err := strconv.Atoi(settings["retention_days"]); err == nil {
		days = d
	}
	affected, err := s.DB.CleanupExpiredLogs(r.Context(), days)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"deleted": affected})
}

func (s *Server) HandleAdminLogsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	filter := db.LogFilter{Status: q.Get("status"), Model: q.Get("model")}
	if from := q.Get("from"); from != "" {
		filter.From, _ = parseTime(from)
	}
	if to := q.Get("to"); to != "" {
		filter.To, _ = parseTime(to)
	}

	logs, err := s.exportAdminLogs(r.Context(), filter)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	format := q.Get("format")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=logs.csv")
		w.Write([]byte("id,created_at,model,status,prompt_tokens,completion_tokens,total_tokens,ttft_ms\n"))
		for _, l := range logs {
			w.Write([]byte(l.ID + "," + l.CreatedAt.Format("2006-01-02T15:04:05Z") + "," + l.Model + "," + l.Status + "," +
				strconv.Itoa(l.PromptTokens) + "," + strconv.Itoa(l.CompletionTokens) + "," + strconv.Itoa(l.TotalTokens) + "," + strconv.Itoa(l.TTFTMs) + "\n"))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=logs.json")
	json.NewEncoder(w).Encode(logs)
}

func (s *Server) HandleAdminStatsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rangeParam := r.URL.Query().Get("range")
	if rangeParam == "" {
		rangeParam = "24h"
	}
	data, err := s.DB.GetDashboardData(r.Context(), rangeParam)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=stats.json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) HandleAdminPoliciesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := s.DB.ListPolicies(r.Context())
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, items)
}

func (s *Server) HandleAdminPoliciesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var policy db.PolicyRule
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(policy.Name) == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "policy name is required")
		return
	}
	if err := s.DB.CreatePolicy(r.Context(), &policy); err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusCreated, policy)
}

func (s *Server) HandleAdminPoliciesUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.WriteMethodNotAllowed(w, http.MethodPut)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/policies/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var policy db.PolicyRule
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	policy.ID = id
	if strings.TrimSpace(policy.Name) == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "policy name is required")
		return
	}
	if err := s.DB.UpdatePolicy(r.Context(), &policy); err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, policy)
}

func (s *Server) HandleAdminPoliciesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		response.WriteMethodNotAllowed(w, http.MethodDelete)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/policies/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.DB.DeletePolicy(r.Context(), id); err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) HandleAdminPoliciesTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Protocol       string `json:"protocol"`
		RequestedModel string `json:"requested_model"`
		Stream         bool   `json:"stream"`
		HasTools       bool   `json:"has_tools"`
		HasReasoning   bool   `json:"has_reasoning"`
		SessionPresent bool   `json:"session_present"`
		ClientName     string `json:"client_name"`
		IngressTag     string `json:"ingress_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	policies, err := s.DB.GetEnabledPolicies(r.Context())
	if err != nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	evaluated, err := policy.EvaluateMatchAttributes(policies, policy.MatchAttributes{
		Protocol:       req.Protocol,
		RequestedModel: req.RequestedModel,
		Stream:         req.Stream,
		HasTools:       req.HasTools,
		HasReasoning:   req.HasReasoning,
		SessionPresent: req.SessionPresent,
		ClientName:     req.ClientName,
		IngressTag:     req.IngressTag,
	})
	if err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if evaluated.EffectiveActions.RewriteModel == nil && req.RequestedModel != "" {
		evaluated.EffectiveActions.RewriteModel = &req.RequestedModel
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"matched":           evaluated.Matched,
		"effective_actions": evaluated.EffectiveActions,
		"matched_rules":     evaluated.MatchedRules,
	})
}

func policyMatchesRequest(match db.PolicyMatch, req struct {
	Protocol       string `json:"protocol"`
	RequestedModel string `json:"requested_model"`
	Stream         bool   `json:"stream"`
	HasTools       bool   `json:"has_tools"`
	HasReasoning   bool   `json:"has_reasoning"`
	SessionPresent bool   `json:"session_present"`
	ClientName     string `json:"client_name"`
	IngressTag     string `json:"ingress_tag"`
}) bool {
	if match.Protocol != "" && match.Protocol != req.Protocol {
		return false
	}
	if match.RequestedModel != "" {
		ok, err := matchRegex(match.RequestedModel, req.RequestedModel)
		if err != nil || !ok {
			return false
		}
	}
	if match.Stream != nil && *match.Stream != req.Stream {
		return false
	}
	if match.HasTools != nil && *match.HasTools != req.HasTools {
		return false
	}
	if match.HasReasoning != nil && *match.HasReasoning != req.HasReasoning {
		return false
	}
	if match.SessionPresent != nil && *match.SessionPresent != req.SessionPresent {
		return false
	}
	if match.ClientName != "" && match.ClientName != req.ClientName {
		return false
	}
	if match.IngressTag != "" && match.IngressTag != req.IngressTag {
		return false
	}
	return true
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func matchRegex(pattern, input string) (bool, error) {
	if len(pattern) > 1024 {
		return false, fmt.Errorf("pattern too long")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(input), nil
}
