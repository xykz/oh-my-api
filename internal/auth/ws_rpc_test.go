package auth

import (
	"os"
	"testing"
	"time"
)

func TestLingmaLoginCallbackMethodName(t *testing.T) {
	if got := lingmaLoginAuthCallbackMethod(); got != "login/auth_callback" {
		t.Fatalf("method = %q, want login/auth_callback", got)
	}
}

func TestLingmaLoginGenerateURLMethodName(t *testing.T) {
	if got := lingmaLoginGenerateURLMethod(); got != "login/generateUrl" {
		t.Fatalf("method = %q, want login/generateUrl", got)
	}
}

func TestProbeLingmaLoginGenerateURL(t *testing.T) {
	if os.Getenv("LINGMA_WS_PROBE") != "1" {
		t.Skip("set LINGMA_WS_PROBE=1 to probe a local Lingma websocket")
	}
	result, err := GenerateLingmaLoginURLViaWebSocket(37010, 10*time.Second)
	if err != nil {
		t.Fatalf("GenerateLingmaLoginURLViaWebSocket: %v", err)
	}
	t.Logf("success=%t nonce_len=%d verifier_len=%d challenge_method=%q login_url=%s",
		result.Success, len(result.Nonce), len(result.Verifier), result.ChallengeMethod, result.LoginURL)
}
