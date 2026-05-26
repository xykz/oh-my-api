package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rizxfrog/oh-my-api/internal/api/response"
)

func (s *Server) HandleAdminAccountBootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	bootstrap := s.Bootstrap
	if bootstrap == nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, "bootstrap not configured")
		return
	}

	switch r.Method {
	case http.MethodPost:
		var body struct {
			Method string `json:"method"`
			Region string `json:"region"`
		}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json: "+err.Error())
				return
			}
		}
		region := strings.ToLower(strings.TrimSpace(body.Region))
		if region == "" {
			region = "china"
		}
		switch region {
		case "china":
		case "international":
			response.WriteOpenAIError(w, http.StatusBadRequest, "international adapter protocol not configured")
			return
		default:
			response.WriteOpenAIError(w, http.StatusBadRequest, "unknown bootstrap region: "+region)
			return
		}
		sess, err := bootstrap.Start(body.Method)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "in progress") {
				status = http.StatusConflict
			}
			response.WriteOpenAIError(w, status, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, sess)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			response.WriteOpenAIError(w, http.StatusBadRequest, "missing id parameter")
			return
		}
		if err := bootstrap.Cancel(id); err != nil {
			status := http.StatusBadRequest
			switch {
			case err.Error() == "session not found":
				status = http.StatusNotFound
			case strings.HasPrefix(err.Error(), "session already"):
				status = http.StatusConflict
			}
			response.WriteOpenAIError(w, status, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})

	default:
		response.WriteMethodNotAllowed(w, "POST, DELETE")
	}
}

func (s *Server) HandleAdminAccountBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	bootstrap := s.Bootstrap
	if bootstrap == nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, "bootstrap not configured")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "missing id parameter")
		return
	}

	sess := bootstrap.GetStatus(id)
	if sess == nil {
		response.WriteOpenAIError(w, http.StatusNotFound, "session not found")
		return
	}

	response.WriteJSON(w, http.StatusOK, sess)
}

func (s *Server) HandleAdminAccountBootstrapSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.isAdminAuthorized(r) {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	bootstrap := s.Bootstrap
	if bootstrap == nil {
		response.WriteOpenAIError(w, http.StatusInternalServerError, "bootstrap not configured")
		return
	}

	var body struct {
		ID          string `json:"id"`
		CallbackURL string `json:"callback_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if body.ID == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "missing id")
		return
	}
	if body.CallbackURL == "" {
		response.WriteOpenAIError(w, http.StatusBadRequest, "missing callback_url")
		return
	}

	sess, err := bootstrap.SubmitCallbackURL(body.ID, body.CallbackURL)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case strings.Contains(err.Error(), "session not found"):
			status = http.StatusNotFound
		case strings.Contains(err.Error(), "already"):
			status = http.StatusConflict
		}
		response.WriteOpenAIError(w, status, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, sess)
}
