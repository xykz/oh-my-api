package router

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"strconv"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/middleware"
)

// New creates the HTTP handler with all routes registered.
func New(deps model.Dependencies, store *db.Store, bootstrap *handler.BootstrapManager) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	s := &handler.Server{
		Deps:               deps,
		DB:                 store,
		StoreExecutionLogs: deps.StoreExecutionLogs,
		Bootstrap:          bootstrap,
		TokenStats:         deps.TokenStats,
		RequestStats:       deps.RequestStats,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.HandleChatCompletions)
	mux.HandleFunc("/v1/messages", s.HandleAnthropicMessages)
	mux.HandleFunc("/v1/responses", s.HandleResponses)
	mux.HandleFunc("/v1/models", s.HandleModels)
	mux.HandleFunc("/admin/status", s.HandleAdminStatus)
	mux.HandleFunc("/admin/overview", s.HandleAdminOverview)
	mux.HandleFunc("/admin/sessions", s.HandleAdminSessions)
	mux.HandleFunc("/admin/sessions/", s.HandleAdminSessionDelete)
	mux.HandleFunc("/admin/dashboard", s.HandleAdminDashboard)
	mux.HandleFunc("/admin/models", s.HandleAdminModels)
	mux.HandleFunc("/admin/account", s.HandleAdminAccount)
	mux.HandleFunc("/admin/account/refresh", s.HandleAdminAccountRefresh)
	mux.HandleFunc("/admin/account/test", s.HandleAdminAccountTest)
	mux.HandleFunc("/admin/account/bootstrap", s.HandleAdminAccountBootstrap)
	mux.HandleFunc("/admin/account/bootstrap/status", s.HandleAdminAccountBootstrapStatus)
	mux.HandleFunc("/admin/account/bootstrap/submit", s.HandleAdminAccountBootstrapSubmit)
	mux.HandleFunc("/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			s.HandleAdminSettingsGet(w, r)
		} else if r.Method == http.MethodPut {
			s.HandleAdminSettingsUpdate(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/admin/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/logs" {
			s.HandleAdminLogsList(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/replay") {
			s.HandleAdminLogsReplay(w, r)
		} else {
			s.HandleAdminLogsGet(w, r)
		}
	})
	mux.HandleFunc("/admin/logs/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/replay") {
			s.HandleAdminLogsReplay(w, r)
		} else {
			s.HandleAdminLogsGet(w, r)
		}
	})
	mux.HandleFunc("/admin/logs/cleanup", s.HandleAdminLogsCleanup)
	mux.HandleFunc("/admin/logs/export", s.HandleAdminLogsExport)
	mux.HandleFunc("/admin/stats/export", s.HandleAdminStatsExport)
	mux.HandleFunc("/admin/policies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			s.HandleAdminPoliciesList(w, r)
		} else if r.Method == http.MethodPost {
			s.HandleAdminPoliciesCreate(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/admin/policies/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/policies/test" {
			s.HandleAdminPoliciesTest(w, r)
			return
		}
		if r.Method == http.MethodPut {
			s.HandleAdminPoliciesUpdate(w, r)
		} else if r.Method == http.MethodDelete {
			s.HandleAdminPoliciesDelete(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	if deps.FrontendFS != (embed.FS{}) {
		subFS, err := fs.Sub(deps.FrontendFS, "frontend-dist")
		if err == nil {
			fileServer := http.FileServerFS(subFS)
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				f, err := subFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
				if err == nil {
					f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
			})
		}
	}

	hdlr := http.Handler(mux)
	if store != nil {
		settings, _ := store.GetSettings(context.Background())
		cfg := middleware.LoggingConfig{
			StorageMode:    settings["storage_mode"],
			TruncateLength: parseIntOr(settings["truncate_length"], 102400),
		}
		hdlr = middleware.Logging(store, cfg)(hdlr)
	}
	return hdlr
}

func parseIntOr(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}
