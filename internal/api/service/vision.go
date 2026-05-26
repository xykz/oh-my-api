package service

import (
	"context"
	"errors"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

// evaluateVisionGate decides whether a canonical request that contains
// image/document blocks should proceed via the soft-fallback path or be
// rejected with model.ErrVisionNotImplemented.
//
// Returns:
//   - (false, nil) when the request has no vision content; caller proceeds normally.
//   - (true, nil)  when fallback is enabled or no settings backend is wired
//     (lightweight/embedded use); caller proceeds and the existing
//     mediaBlockToText projection compresses images into text.
//   - (false, model.ErrVisionNotImplemented) when fallback is explicitly disabled OR
//     when the settings store fails (conservative).
func EvaluateVisionGate(ctx context.Context, store model.SettingsStore, req proxy.CanonicalRequest) (bool, error) {
	if !canonicalRequestHasVisionContent(req) {
		return false, nil
	}
	if store == nil {
		// No settings backend wired (e.g., legacy/embedded callers, lightweight
		// tests). Preserve legacy soft-fallback semantics to avoid breaking
		// existing consumers; explicit production deployments wire a real store
		// and can opt into 501 by leaving vision_fallback_enabled=false.
		return true, nil
	}
	settings, err := store.GetSettings(ctx)
	if err != nil {
		return false, model.ErrVisionNotImplemented
	}
	if settings["vision_fallback_enabled"] == "true" {
		return true, nil
	}
	return false, model.ErrVisionNotImplemented
}

func canonicalRequestHasVisionContent(req proxy.CanonicalRequest) bool {
	for _, turn := range req.Turns {
		for _, block := range turn.Blocks {
			if block.Type == proxy.CanonicalBlockImage || block.Type == proxy.CanonicalBlockDocument {
				return true
			}
		}
	}
	return false
}

// IsVisionNotImplemented returns true if err is the vision-not-implemented sentinel.
func IsVisionNotImplemented(err error) bool {
	return errors.Is(err, model.ErrVisionNotImplemented)
}
