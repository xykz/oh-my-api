package handler

import (
	"net/http"

	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func (s *Server) HandleModels(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteMethodNotAllowed(writer, http.MethodGet)
		return
	}

	models, err := s.Deps.Models.ListModels(request.Context())
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}

	response.WriteJSON(writer, http.StatusOK, proxy.OpenAIModelsResponse{
		Object: "list",
		Data:   models,
	})
}
