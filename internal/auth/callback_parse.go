package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CallbackV2Result holds the decoded fields from a V2 (or V1-fallback) callback.
type CallbackV2Result struct {
	UID                string
	AID                string
	Name               string
	SecurityOAuthToken string
	RefreshToken       string
	ExpireTime         int64  // unix millis, 0 = unknown
	ExpireTimeRaw      string // original string if parsing failed
}

// ParseCallbackV2 tries to parse query parameters as a V2 callback first
// (auth=<Encode1> + token=<Encode1>), then falls back to plain token fields
// and finally to V1 flat params.
func ParseCallbackV2(query url.Values) (*CallbackV2Result, error) {
	result := &CallbackV2Result{}

	if auth := query.Get("auth"); auth != "" {
		parts, err := decodeCallbackAuthParts(auth)
		if err != nil {
			logCallbackDecodeFailure("auth", auth, err)
			if tokenParam := query.Get("token"); tokenParam != "" {
				logCallbackDecodeFailure("token", tokenParam, fmt.Errorf("auth decode failed; token diagnostic only"))
			}
			return nil, fmt.Errorf("v2 auth decode failed: %w", err)
		}
		result.UID = parts[0]
		if len(parts) >= 3 {
			result.AID = parts[1]
			result.Name = parts[2]
		} else {
			result.Name = parts[1]
		}
	}

	if tokenParam := query.Get("token"); tokenParam != "" {
		parts, err := decodeCallbackTokenParts(tokenParam)
		if err != nil {
			logCallbackDecodeFailure("token", tokenParam, err)
			return nil, fmt.Errorf("v2 token decode failed: %w", err)
		}
		result.SecurityOAuthToken = parts[0]
		result.RefreshToken = parts[1]
		result.ExpireTimeRaw = parts[2]
		if v, parseErr := strconv.ParseInt(parts[2], 10, 64); parseErr == nil {
			result.ExpireTime = v
		}
	}

	if result.UID != "" && result.SecurityOAuthToken != "" {
		return result, nil
	}

	if uid := query.Get("uid"); uid != "" {
		result.UID = uid
	}
	if aid := query.Get("aid"); aid != "" {
		result.AID = aid
	}
	if name := query.Get("name"); name != "" {
		result.Name = name
	}
	if accessToken := query.Get("access_token"); accessToken != "" {
		result.SecurityOAuthToken = accessToken
	}
	if refreshToken := query.Get("refresh_token"); refreshToken != "" {
		result.RefreshToken = refreshToken
	}
	if expireTime := query.Get("expire_time"); expireTime != "" {
		result.ExpireTimeRaw = expireTime
		if v, err := strconv.ParseInt(expireTime, 10, 64); err == nil {
			result.ExpireTime = v
		}
	}

	if result.UID != "" && result.SecurityOAuthToken != "" {
		return result, nil
	}
	if result.UID != "" {
		return result, nil
	}

	return nil, fmt.Errorf("callback contains neither V2 (auth+token) nor V1/plain token parameters")
}

// ParseCallbackV2FromURL parses a full callback URL string.
func ParseCallbackV2FromURL(rawURL string) (*CallbackV2Result, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("callback url is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		log.Printf("[callback-debug] url.Parse failed raw_url_len=%d err_type=%T", len(trimmed), err)
		if result, rawErr := parseRawCallbackV2Params(trimmed); rawErr == nil {
			return result, nil
		}
		return nil, fmt.Errorf("parse callback url: %w", err)
	}

	// The Encode=1 alphabet includes '#', which url.Parse treats as a fragment
	// delimiter. Reconstruct the full raw query by re-appending the fragment.
	rawQuery := parsed.RawQuery
	if parsed.Fragment != "" {
		rawQuery += "#" + parsed.Fragment
	}
	if rawQuery == "" {
		return nil, fmt.Errorf("callback url missing query parameters")
	}

	result, err := ParseCallbackV2(parsed.Query())
	if err == nil {
		return result, nil
	}
	log.Printf("[callback-debug] parsed query decode failed raw_query_len=%d fragment_len=%d keys=%v err=%v",
		len(parsed.RawQuery), len(parsed.Fragment), queryKeys(parsed.Query()), err)
	if rawResult, rawErr := parseRawCallbackV2Params(trimmed); rawErr == nil {
		return rawResult, nil
	}
	return nil, err
}

// ParseCallbackV2FromStrings decodes Encode=1 auth/token strings directly.
func ParseCallbackV2FromStrings(authParam, tokenParam string) (*CallbackV2Result, error) {
	result := &CallbackV2Result{}

	if authParam != "" {
		parts, err := decodeCallbackAuthParts(authParam)
		if err != nil {
			logCallbackDecodeFailure("auth", authParam, err)
			if tokenParam != "" {
				logCallbackDecodeFailure("token", tokenParam, fmt.Errorf("auth decode failed; token diagnostic only"))
			}
			return nil, fmt.Errorf("v2 auth decode failed: %w", err)
		}
		result.UID = parts[0]
		if len(parts) >= 3 {
			result.AID = parts[1]
			result.Name = parts[2]
		} else {
			result.Name = parts[1]
		}
	}

	if tokenParam != "" {
		parts, err := decodeCallbackTokenParts(tokenParam)
		if err != nil {
			logCallbackDecodeFailure("token", tokenParam, err)
			return nil, fmt.Errorf("v2 token decode failed: %w", err)
		}
		result.SecurityOAuthToken = parts[0]
		result.RefreshToken = parts[1]
		result.ExpireTimeRaw = parts[2]
		if v, parseErr := strconv.ParseInt(parts[2], 10, 64); parseErr == nil {
			result.ExpireTime = v
		}
	}

	if result.UID == "" || result.SecurityOAuthToken == "" {
		return nil, fmt.Errorf("v2 callback missing uid or token after decode")
	}

	return result, nil
}

func RawCallbackAuthTokenFromURL(rawURL string) (authParam, tokenParam string, ok bool) {
	queryStart := strings.IndexByte(rawURL, '?')
	if queryStart < 0 || queryStart == len(rawURL)-1 {
		return "", "", false
	}
	return extractRawAuthToken(rawURL[queryStart+1:])
}

func CallbackHasBinaryTokenString(rawURL string) bool {
	_, tokenParam, ok := RawCallbackAuthTokenFromURL(rawURL)
	if !ok {
		return false
	}
	raw, err := DecodeString(tokenParam)
	if err != nil {
		return false
	}
	binaryTokenLike, tokenExpireLike := detectBinaryCallbackShape(raw)
	return binaryTokenLike && tokenExpireLike
}

func decodeCallbackAuthParts(encoded string) ([]string, error) {
	raw, err := DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	parts := strings.Split(string(raw), "\n")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, fmt.Errorf("expected 2 or 3 parts, got %d", len(parts))
	}
	if parts[0] == "" {
		return nil, fmt.Errorf("missing uid")
	}
	return parts, nil
}

func decodeCallbackTokenParts(encoded string) ([]string, error) {
	raw, err := DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !isMostlyPrintableUTF8(raw) {
		// todo: 😅simplify these try tree...
		return nil, fmt.Errorf("binary tokenString callback")
	}
	text := string(raw)
	parts := strings.SplitN(text, "\n", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("expected 3 parts, got %d", len(parts))
	}
	if !strings.HasPrefix(parts[0], "pt-") || !strings.HasPrefix(parts[1], "rt-") {
		return nil, fmt.Errorf("binary tokenString callback")
	}
	return parts, nil
}

func detectBinaryCallbackShape(raw []byte) (bool, bool) {
	parts := strings.Split(string(raw), "\n")
	if len(parts) != 2 {
		return false, false
	}
	expire := parts[1]
	if len(expire) != 13 {
		return true, false
	}
	_, err := strconv.ParseInt(expire, 10, 64)
	return true, err == nil
}

func parseRawCallbackV2Params(rawURL string) (*CallbackV2Result, error) {
	queryStart := strings.IndexByte(rawURL, '?')
	if queryStart < 0 || queryStart == len(rawURL)-1 {
		return nil, fmt.Errorf("callback url missing query parameters")
	}

	// Use everything after '?' as the raw query. Do NOT strip at '#' because
	// the Encode=1 alphabet includes '#' as a valid character.
	rawQuery := rawURL[queryStart+1:]
	authParam, tokenParam, ok := extractRawAuthToken(rawQuery)
	if !ok {
		log.Printf("[callback-debug] raw query extraction failed raw_query_len=%d fields=%d keys=%v",
			len(rawQuery), len(splitOnKnownKeys(rawQuery)), rawFieldKeys(rawQuery))
		return nil, fmt.Errorf("callback contains neither raw auth nor raw token parameters")
	}
	return ParseCallbackV2FromStrings(authParam, tokenParam)
}

// knownCallbackKeys are the only query parameter keys expected in a Lingma
// callback URL. The Encode=1 alphabet includes '&' and '#', so a naive split
// on '&' would truncate encoded values that contain literal '&'. We only split
// on '&' when it is immediately followed by one of these known keys + '='.
var knownCallbackKeys = []string{"auth=", "token=", "state="}

func extractRawAuthToken(rawQuery string) (authParam, tokenParam string, ok bool) {
	// Split the raw query into fields only at '&' boundaries that precede a
	// known key. This preserves literal '&' characters inside Encode=1 values.
	fields := splitOnKnownKeys(rawQuery)
	for _, field := range fields {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		switch key {
		case "auth":
			authParam = value
		case "token":
			tokenParam = value
		}
	}
	return authParam, tokenParam, authParam != "" && tokenParam != ""
}

func rawFieldKeys(rawQuery string) []string {
	fields := splitOnKnownKeys(rawQuery)
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		key, _, found := strings.Cut(field, "=")
		if found {
			keys = append(keys, key)
		}
	}
	return keys
}

// splitOnKnownKeys splits rawQuery on '&' only when the text after '&' starts
// with a known callback key (e.g. "auth=", "token=", "state="). Literal '&'
// characters from the Encode=1 alphabet that appear inside values are preserved.
func splitOnKnownKeys(rawQuery string) []string {
	var fields []string
	start := 0
	for i := 0; i < len(rawQuery); i++ {
		if rawQuery[i] != '&' {
			continue
		}
		rest := rawQuery[i+1:]
		isBoundary := false
		for _, k := range knownCallbackKeys {
			if strings.HasPrefix(rest, k) {
				isBoundary = true
				break
			}
		}
		if isBoundary {
			fields = append(fields, rawQuery[start:i])
			start = i + 1
		}
	}
	fields = append(fields, rawQuery[start:])
	return fields
}

func logCallbackDecodeFailure(label, encoded string, cause error) {
	raw, err := DecodeString(encoded)
	if err != nil {
		log.Printf("[callback-debug] %s decode failed encoded_len=%d err=%v cause=%v", label, len(encoded), err, cause)
		return
	}
	parts := strings.Split(string(raw), "\n")
	partLens := make([]int, len(parts))
	for i, part := range parts {
		partLens[i] = len(part)
	}
	sum := sha256.Sum256(raw)
	binaryTokenLike, tokenExpireLike := detectBinaryCallbackShape(raw)
	log.Printf("[callback-debug] %s decoded unexpected encoded_len=%d decoded_len=%d parts=%d part_lens=%v printable=%t sha256=%s cause=%v",
		label,
		len(encoded),
		len(raw),
		len(parts),
		partLens,
		isMostlyPrintableUTF8(raw),
		hex.EncodeToString(sum[:])[:12],
		cause,
	)
	log.Printf("[callback-debug] %s decoded shape has_lf=%t printable_runs=%v binary_token_like=%t token_expire_like=%t",
		label, strings.Contains(string(raw), "\n"), printableRuns(raw), binaryTokenLike, tokenExpireLike)
	if binaryTokenLike {
		log.Printf("[callback-debug] %s appears to be Lingma IDE tokenString/auth binary shape; plaintext uid/token parser cannot consume it directly expire_like=%t",
			label, tokenExpireLike)
	}
}

func isMostlyPrintableUTF8(raw []byte) bool {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return false
	}
	printable := 0
	for _, r := range string(raw) {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsPrint(r) {
			printable++
		}
	}
	return printable*100/utf8.RuneCount(raw) >= 90
}

func queryKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func printableRuns(raw []byte) []int {
	runs := make([]int, 0, 4)
	current := 0
	for _, b := range raw {
		if b >= 0x20 && b <= 0x7e {
			current++
			continue
		}
		if current >= 4 {
			runs = append(runs, current)
		}
		current = 0
	}
	if current >= 4 {
		runs = append(runs, current)
	}
	if len(runs) > 6 {
		return runs[:6]
	}
	return runs
}
