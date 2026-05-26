package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/policy"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func RequestSessionID(request *http.Request, bodySessionID string) string {
	sessionID := strings.TrimSpace(bodySessionID)
	if headerSession := strings.TrimSpace(request.Header.Get("X-Session-Id")); headerSession != "" {
		sessionID = headerSession
	}
	return sessionID
}

func AttachCanonicalRequestMetadata(canonical *proxy.CanonicalRequest, headers http.Header) {
	if canonical.Metadata == nil {
		canonical.Metadata = map[string]any{}
	}
	if clientName := strings.TrimSpace(headers.Get("X-Client-Name")); clientName != "" {
		canonical.Metadata["client_name"] = clientName
	}
	if ingressTag := strings.TrimSpace(headers.Get("X-Ingress-Tag")); ingressTag != "" {
		canonical.Metadata["ingress_tag"] = ingressTag
	}
	if replayMode := strings.TrimSpace(headers.Get("X-Replay-Mode")); replayMode != "" {
		canonical.Metadata["replay_mode"] = replayMode
	}
}

// EvaluateCanonicalRequest evaluates all enabled policies against the canonical request.
// It needs a *db.Store (or nil for no-policy mode) and respects replay_mode=historical.
func EvaluateCanonicalRequest(ctx context.Context, store *db.Store, canonical proxy.CanonicalRequest) (policy.EvaluationResult, error) {
	if replayMode, _ := canonical.Metadata["replay_mode"].(string); strings.EqualFold(replayMode, "historical") {
		return policy.EvaluationResult{
			Matched:           false,
			EffectiveActions:  policy.EvaluationResult{}.EffectiveActions,
			PostPolicyRequest: canonical,
		}, nil
	}
	if store == nil {
		return policy.EvaluationResult{
			Matched:           false,
			EffectiveActions:  policy.EvaluationResult{}.EffectiveActions,
			PostPolicyRequest: canonical,
		}, nil
	}

	rules, err := store.GetEnabledPolicies(ctx)
	if err != nil {
		return policy.EvaluationResult{}, err
	}
	return policy.EvaluateCanonicalRequest(rules, canonical)
}

// PersistCanonicalExecutionRecord saves a canonical execution record to the database.
func PersistCanonicalExecutionRecord(
	ctx context.Context,
	store *db.Store,
	now time.Time,
	storeEnabled bool,
	traceID string,
	ingressProtocol proxy.CanonicalProtocol,
	ingressEndpoint string,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	projectedRequest proxy.OpenAIChatRequest,
	requestMessages []proxy.Message,
	assistant proxy.Message,
	remoteRequest proxy.RemoteChatRequest,
	rawSSELines []string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
) {
	if store == nil {
		return
	}
	if !storeEnabled {
		return
	}

	// Fallback to estimation if no real counts provided
	if promptTokens <= 0 {
		promptTokens = db.EstimateMessageTokens(requestMessages)
	}
	if completionTokens <= 0 {
		completionTokens = db.EstimateMessageTokens([]proxy.Message{assistant})
	}
	if totalTokens <= 0 {
		totalTokens = promptTokens + completionTokens
	}

	record := &db.CanonicalExecutionRecordRow{
		ID:                traceID,
		CreatedAt:         now,
		IngressProtocol:   string(ingressProtocol),
		IngressEndpoint:   ingressEndpoint,
		SessionID:         postPolicyRequest.SessionID,
		PrePolicyRequest:  prePolicyRequest,
		PostPolicyRequest: postPolicyRequest,
		SessionSnapshot: BuildCanonicalSessionSnapshot(
			ingressProtocol,
			sessionCanonicalRequest,
			assistant,
			now,
		),
		SouthboundRequest: remoteRequest.BodyJSON,
		Sidecar: &proxy.CanonicalExecutionSidecar{
			SchemaVersion: 1,
			RawSSELines:   rawSSELines,
			Metadata: map[string]any{
				"request_id":        remoteRequest.RequestID,
				"model_key":         remoteRequest.ModelKey,
				"stream":            remoteRequest.Stream,
				"prompt_tokens":     promptTokens,
				"completion_tokens": completionTokens,
				"total_tokens":      totalTokens,
			},
		},
	}
	_ = store.InsertCanonicalExecutionRecord(ctx, record)
}

func BuildCanonicalSessionSnapshot(
	ingressProtocol proxy.CanonicalProtocol,
	postPolicyRequest proxy.CanonicalRequest,
	assistant proxy.Message,
	now time.Time,
) *proxy.CanonicalSessionSnapshot {
	assistantCanonical, err := proxy.CanonicalizeOpenAIRequest(proxy.OpenAIChatRequest{
		Messages: []proxy.Message{assistant},
	}, postPolicyRequest.SessionID)
	if err != nil {
		return nil
	}
	turns := make([]proxy.CanonicalTurn, 0, len(postPolicyRequest.Turns)+len(assistantCanonical.Turns))
	turns = append(turns, postPolicyRequest.Turns...)
	turns = append(turns, assistantCanonical.Turns...)

	return &proxy.CanonicalSessionSnapshot{
		SchemaVersion:   1,
		SessionID:       postPolicyRequest.SessionID,
		IngressProtocol: ingressProtocol,
		Turns:           turns,
		UpdatedAt:       now,
	}
}

func CloneMetadataMap(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
