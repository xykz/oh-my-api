package routes

import (
	"net/http"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
)

func registerCodeBuddyRoutes(mux *http.ServeMux, s *handler.Server) {
	mux.HandleFunc("/codebuddy/v1/chat/completions", s.HandleCodeBuddyChat)
	mux.HandleFunc("/codebuddy/v1/models", s.HandleCodeBuddyModels)
}
