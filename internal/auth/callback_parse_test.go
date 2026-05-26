package auth

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestParseCallbackV2AcceptsTwoPartAuth(t *testing.T) {
	authParam := LingmaEncode([]byte("user-123\nalice"))
	tokenParam := LingmaEncode([]byte("pt-test\nrt-test\n1782107060847"))

	result, err := ParseCallbackV2(url.Values{
		"auth":  []string{authParam},
		"token": []string{tokenParam},
	})
	if err != nil {
		t.Fatalf("ParseCallbackV2: %v", err)
	}

	if result.UID != "user-123" {
		t.Fatalf("UID: got %q, want user-123", result.UID)
	}
	if result.AID != "" {
		t.Fatalf("AID: got %q, want empty", result.AID)
	}
	if !strings.HasPrefix(result.Name, "alice") {
		t.Fatalf("Name: got %q, want alice prefix", result.Name)
	}
	if result.SecurityOAuthToken != "pt-test" {
		t.Fatalf("SecurityOAuthToken: got %q, want pt-test", result.SecurityOAuthToken)
	}
}

func TestParseCallbackV2FromURLAcceptsUnescapedEncode1QueryValues(t *testing.T) {
	authParam := encode1WithAnyReservedURLChar("user-123\nalice")
	tokenParam := encode1WithAnyReservedURLChar("pt-test\nrt-test\n1782107060847")
	rawURL := fmt.Sprintf("http://127.0.0.1:37510/auth/callback?auth=%s&token=%s", authParam, tokenParam)

	result, err := ParseCallbackV2FromURL(rawURL)
	if err != nil {
		t.Fatalf("ParseCallbackV2FromURL: %v", err)
	}

	if result.UID != "user-123" {
		t.Fatalf("UID: got %q, want user-123", result.UID)
	}
	if !strings.HasPrefix(result.Name, "alice") {
		t.Fatalf("Name: got %q, want alice prefix", result.Name)
	}
	if result.SecurityOAuthToken != "pt-test" {
		t.Fatalf("SecurityOAuthToken: got %q, want pt-test", result.SecurityOAuthToken)
	}
}

func TestExtractRawAuthTokenAllowsStateBetweenAuthAndToken(t *testing.T) {
	authParam := LingmaEncode([]byte("user-123\nalice"))
	tokenParam := LingmaEncode([]byte("pt-test\nrt-test\n1782107060847"))
	rawQuery := fmt.Sprintf("auth=%s&state=abc&token=%s", url.QueryEscape(authParam), url.QueryEscape(tokenParam))

	gotAuth, gotToken, ok := extractRawAuthToken(rawQuery)
	if !ok {
		t.Fatal("extractRawAuthToken returned ok=false")
	}
	if gotAuth != authParam {
		t.Fatalf("auth: got %q, want %q", gotAuth, authParam)
	}
	if gotToken != tokenParam {
		t.Fatalf("token: got %q, want %q", gotToken, tokenParam)
	}
}

func TestExtractRawAuthTokenPreservesReservedEncodeChars(t *testing.T) {
	authParam := encode1WithAllReservedURLChars("user-123\nalice")
	tokenParam := encode1WithAllReservedURLChars("pt-test\nrt-test\n1782107060847")
	rawQuery := fmt.Sprintf("auth=%s&state=abc&token=%s", authParam, tokenParam)

	gotAuth, gotToken, ok := extractRawAuthToken(rawQuery)
	if !ok {
		t.Fatal("extractRawAuthToken returned ok=false")
	}
	if gotAuth != authParam {
		t.Fatalf("auth: got %q, want %q", gotAuth, authParam)
	}
	if gotToken != tokenParam {
		t.Fatalf("token: got %q, want %q", gotToken, tokenParam)
	}
}

func TestCallbackHasBinaryTokenString(t *testing.T) {
	authParam := LingmaEncode([]byte("binary-auth-placeholder"))
	tokenParam := LingmaEncode(append([]byte{0, 1, 2, 3, '\n'}, []byte("1782107060847")...))
	rawURL := fmt.Sprintf("http://127.0.0.1:37510/auth/callback?auth=%s&state=abc&token=%s", authParam, tokenParam)

	if !CallbackHasBinaryTokenString(rawURL) {
		t.Fatal("expected binary tokenString shape")
	}
}

func TestDecodeCallbackTokenPartsReportsBinaryTokenString(t *testing.T) {
	tokenParam := "l@N^oECQPHrIjRV.lp$$Vfn%jR_kVRp.l@&%jR&%V@pfj^_wj@zIV@pIV@zk"

	_, err := decodeCallbackTokenParts(tokenParam)
	if err == nil || !strings.Contains(err.Error(), "binary tokenString") {
		t.Fatalf("expected binary tokenString error, got %v", err)
	}
}

func TestCallbackHasBinaryTokenStringDetectsBinaryAuth(t *testing.T) {
	authParam := url.QueryEscape(LingmaEncode([]byte("binary-auth-placeholder")))
	tokenParam := url.QueryEscape(LingmaEncode(append([]byte{0, 1, 2, 3, '\n'}, []byte("1782107060847")...)))
	rawURL := fmt.Sprintf("http://127.0.0.1:23117/auth/callback?state=abc&auth=%s&token=%s", authParam, tokenParam)

	if !CallbackHasBinaryTokenString(rawURL) {
		t.Fatal("expected binary Lingma callback shape")
	}
}

func encode1WithAnyReservedURLChar(prefix string) string {
	for i := 0; i < 1000; i++ {
		encoded := LingmaEncode([]byte(fmt.Sprintf("%s-%d", prefix, i)))
		if strings.ContainsAny(encoded, "#&%") {
			return encoded
		}
	}
	panic("could not generate Encode=1 value containing URL-reserved characters")
}

func encode1WithAllReservedURLChars(prefix string) string {
	for i := 0; i < 10000; i++ {
		encoded := LingmaEncode([]byte(fmt.Sprintf("%s-%d", prefix, i)))
		if strings.Contains(encoded, "#") && strings.Contains(encoded, "&") {
			return encoded
		}
	}
	panic("could not generate Encode=1 value containing # and &")
}
