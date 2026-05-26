package handler

import (
	"net/http"
	"strings"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/api/response"
)

func (s *Server) HandleAdminStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteMethodNotAllowed(writer, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(request) {
		response.WriteOpenAIError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	sessions, err := s.Deps.Sessions.List(request.Context())
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	response.WriteJSON(writer, http.StatusOK, model.AdminStatusResponse{
		Credential:   s.Deps.Credentials.Status(),
		Models:       s.Deps.Models.Status(),
		SessionCount: len(sessions),
	})
}

func (s *Server) HandleAdminSessions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteMethodNotAllowed(writer, http.MethodGet)
		return
	}
	if !s.isAdminAuthorized(request) {
		response.WriteOpenAIError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	sessions, err := s.Deps.Sessions.List(request.Context())
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	response.WriteJSON(writer, http.StatusOK, sessions)
}

func (s *Server) HandleAdminSessionDelete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodDelete {
		response.WriteMethodNotAllowed(writer, http.MethodDelete)
		return
	}
	if !s.isAdminAuthorized(request) {
		response.WriteOpenAIError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	sessionID := strings.TrimPrefix(request.URL.Path, "/admin/sessions/")
	if sessionID == "" || sessionID == request.URL.Path {
		response.WriteOpenAIError(writer, http.StatusBadRequest, "missing session id")
		return
	}
	if err := s.Deps.Sessions.Delete(request.Context(), sessionID); err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	response.WriteJSON(writer, http.StatusOK, map[string]string{"status": "deleted"})
}
