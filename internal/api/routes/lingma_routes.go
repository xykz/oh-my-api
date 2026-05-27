package routes

import (
	"net/http"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
)

func registerLingmaRoutes(mux *http.ServeMux, s *handler.Server) {
	mux.HandleFunc("/lingma/v1/chat/completions", s.HandleChatCompletions)
	mux.HandleFunc("/lingma/v1/messages", s.HandleAnthropicMessages)
	mux.HandleFunc("/lingma/v1/responses", s.HandleResponses)
	mux.HandleFunc("/lingma/v1/models", s.HandleModels)
}
