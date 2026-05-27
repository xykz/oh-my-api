package routes

import (
	"net/http"
	"strings"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
)

func registerAdminRoutes(mux *http.ServeMux, s *handler.Server) {
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
}
