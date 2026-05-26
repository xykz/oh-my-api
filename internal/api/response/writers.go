package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func WriteSSEChunk(writer http.ResponseWriter, payload model.ChatCompletionResponse) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "data: %s\n\n", data)
	return err
}

func WriteJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

func WriteMethodNotAllowed(writer http.ResponseWriter, method string) {
	writer.Header().Set("Allow", method)
	WriteOpenAIError(writer, http.StatusMethodNotAllowed, "method not allowed")
}

func WriteMappedError(writer http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	switch {
	case errors.Is(err, proxy.ErrUnknownModel):
		statusCode = http.StatusBadRequest
	case errors.Is(err, proxy.ErrAdapterProtocolNotConfigured):
		statusCode = http.StatusNotImplemented
	case errors.Is(err, proxy.ErrCredentialsUnavailable):
		statusCode = http.StatusInternalServerError
	default:
		var upstream *proxy.UpstreamHTTPError
		if errors.As(err, &upstream) {
			if upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden {
				statusCode = http.StatusUnauthorized
			} else {
				statusCode = http.StatusBadGateway
			}
		}
	}
	WriteOpenAIError(writer, statusCode, err.Error())
}

func WriteOpenAIError(writer http.ResponseWriter, statusCode int, message string) {
	WriteJSON(writer, statusCode, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func WriteOpenAIVisionNotImplemented(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusNotImplemented)
	body := map[string]any{
		"error": map[string]any{
			"message": "vision input is not implemented yet; set vision_fallback_enabled=true in settings to fall back to text representation",
			"type":    "not_implemented",
			"code":    "vision_not_implemented",
		},
	}
	_ = json.NewEncoder(writer).Encode(body)
}

func WriteOpenAIInvalidImage(writer http.ResponseWriter, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusBadRequest)
	body := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"code":    "invalid_image",
		},
	}
	_ = json.NewEncoder(writer).Encode(body)
}

// ── Anthropic response helpers ──────────────────────────────────

func WriteAnthropicError(writer http.ResponseWriter, statusCode int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": "invalid_request_error", "message": message},
	})
}

func WriteAnthropicMappedError(writer http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	switch {
	case errors.Is(err, proxy.ErrUnknownModel):
		statusCode = http.StatusBadRequest
	case errors.Is(err, proxy.ErrAdapterProtocolNotConfigured):
		statusCode = http.StatusNotImplemented
	case errors.Is(err, proxy.ErrCredentialsUnavailable):
		statusCode = http.StatusInternalServerError
	default:
		var upstream *proxy.UpstreamHTTPError
		if errors.As(err, &upstream) {
			if upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden {
				statusCode = http.StatusUnauthorized
			} else {
				statusCode = http.StatusBadGateway
			}
		}
	}
	WriteAnthropicError(writer, statusCode, err.Error())
}

func WriteAnthropicVisionNotImplemented(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusNotImplemented)
	body := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "not_supported_yet",
			"message": "vision input is not implemented yet; set vision_fallback_enabled=true in settings to fall back to text representation",
		},
	}
	_ = json.NewEncoder(writer).Encode(body)
}

func WriteAnthropicInvalidImage(writer http.ResponseWriter, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusBadRequest)
	body := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
		},
	}
	_ = json.NewEncoder(writer).Encode(body)
}

// ── Response API helpers ──────────────────────────────────

func WriteResponseError(writer http.ResponseWriter, statusCode int, reason string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(proxy.OpenAIResponse{
		Object: "response",
		Status: "failed",
		StatusDetails: &proxy.ResponseStatusDetails{
			Type:   "invalid_request_error",
			Reason: reason,
		},
	})
}

func WriteResponseSSE(writer http.ResponseWriter, event proxy.ResponseStreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, data)
}
