package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/auth"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

const remoteCallbackTimeout = 5 * time.Minute

var remoteCallbackTimeoutForTest = remoteCallbackTimeout
var startLingmaLoginSession = auth.StartLingmaLoginSession
var generateLingmaLoginURLViaWebSocket = auth.GenerateLingmaLoginURLViaWebSocket
var submitLingmaLoginCallbackViaWebSocket = auth.SubmitLingmaLoginCallbackViaWebSocket
var tryImportFromLingmaCache = auth.TryImportFromLingmaCache

func (m *BootstrapManager) StartRemoteCallback() (*BootstrapSession, error) {
	m.mu.Lock()
	if existing := m.findActiveLocked(); existing != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("another bootstrap in progress (id=%s)", existing.ID)
	}

	listenAddr := m.listenAddr
	if m.AutoDetectFreePort {
		var err error
		listenAddr, err = resolveListenAddr(m.listenAddr)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("resolve callback addr: %w", err)
		}
	}

	port, err := portFromAddr(listenAddr)
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("invalid callback addr: %w", err)
	}

	cacheMTime := lingmaCacheUserModTime()
	machineID, loginURL, state, verifier, nonce, lingmaRPC, err := m.buildRemoteLoginURL(port, listenAddr)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	browserURL, err := browserURLForGeneratedLingmaLoginURL(loginURL)
	if err != nil {
		if lingmaRPC != nil {
			lingmaRPC.Close()
		}
		m.mu.Unlock()
		return nil, fmt.Errorf("wrap login url: %w", err)
	}

	timeout := remoteCallbackTimeoutForTest
	now := time.Now()
	id := newSessionID()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	sess := &BootstrapSession{
		ID:         id,
		Status:     "awaiting_callback_url",
		Method:     "remote_callback",
		AuthURL:    browserURL,
		StartedAt:  now,
		ExpiresAt:  now.Add(timeout),
		cancel:     cancel,
		machineID:  machineID,
		state:      state,
		verifier:   verifier,
		nonce:      nonce,
		lingmaRPC:  lingmaRPC,
		listenAddr: listenAddr,
		cacheMTime: cacheMTime,
	}
	m.sessions[id] = sess
	snapshot := *sess
	snapshot.cancel = nil
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		if ctx.Err() != context.DeadlineExceeded {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		current, ok := m.sessions[id]
		if !ok || current.Status != "awaiting_callback_url" {
			return
		}
		current.Status = "error"
		current.Error = "timeout: user did not complete login within 5m"
		current.Phase = ""
		closeLingmaLoginSession(current)
		current.cancel = nil
	}()

	go m.runRemoteCallbackFlow(ctx, id, machineID, listenAddr)

	return &snapshot, nil
}

func (m *BootstrapManager) buildRemoteLoginURL(port, listenAddr string) (machineID, loginURL, state, verifier, nonce string, lingmaRPC *auth.LingmaLoginSession, err error) {
	if session, sessionErr := startLingmaLoginSession(37010, 30*time.Second); sessionErr == nil {
		generated, genErr := session.GenerateURL()
		if genErr != nil {
			session.Close()
			log.Printf("[callback-debug] Lingma persistent login/generateUrl failed; using local login URL builder err=%v", genErr)
		} else {
			loginURL = generated.LoginURL
			state = queryValueFromPossiblyEscapedURL(loginURL, "state")
			verifier = generated.Verifier
			nonce = generated.Nonce
			if nonce == "" {
				nonce = queryValueFromPossiblyEscapedURL(loginURL, "nonce")
			}
			machineID = queryValueFromPossiblyEscapedURL(loginURL, "machine_id")
			if machineID == "" {
				machineID = auth.NewMachineID()
			}
			log.Printf("[callback-debug] using persistent Lingma login/generateUrl url_len=%d state_len=%d nonce_len=%d verifier_len=%d machine_hash=%s",
				len(loginURL), len(state), len(nonce), len(verifier), shortHash(machineID))
			return machineID, loginURL, state, verifier, nonce, session, nil
		}
	} else {
		log.Printf("[callback-debug] Lingma persistent login session unavailable; using local login URL builder err=%v", sessionErr)
	}

	machineID = auth.NewMachineID()
	loginURL, state, verifier, err = auth.BuildLingmaLoginEntryURL(auth.LingmaLoginEntryConfig{
		MachineID: machineID,
		Port:      port,
	})
	if err != nil {
		return "", "", "", "", "", nil, fmt.Errorf("build lingma login url: %w", err)
	}
	nonce = queryValueFromPossiblyEscapedURL(loginURL, "nonce")
	return machineID, loginURL, state, verifier, nonce, nil, nil
}

func (m *BootstrapManager) SubmitCallbackURL(id, rawURL string) (*BootstrapSession, error) {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse callback url: %w", err)
	}
	if parsedURL.Scheme != "http" {
		return nil, fmt.Errorf("callback url must use http")
	}

	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session not found")
	}
	expectedHost := sess.listenAddr
	if expectedHost == "" {
		expectedHost = m.listenAddr
	}
	if parsedURL.Host != expectedHost {
		m.mu.Unlock()
		return nil, fmt.Errorf("callback url host must be %s", expectedHost)
	}
	if !sess.ExpiresAt.IsZero() && time.Now().After(sess.ExpiresAt) {
		sess.Status = "error"
		sess.Phase = ""
		sess.Error = "timeout: user did not complete login within 5m"
		snapshot := *sess
		snapshot.cancel = nil
		m.mu.Unlock()
		return &snapshot, fmt.Errorf("%s", sess.Error)
	}
	if sess.Status == "cancelled" {
		m.mu.Unlock()
		return nil, fmt.Errorf("session already cancelled")
	}
	if sess.Status == "completed" {
		m.mu.Unlock()
		return nil, fmt.Errorf("session already completed")
	}
	if sess.Status != "awaiting_callback_url" && sess.Status != "error" {
		m.mu.Unlock()
		return nil, fmt.Errorf("session already %s", sess.Status)
	}
	machineID := sess.machineID
	state := sess.state
	verifier := sess.verifier
	nonce := sess.nonce
	sess.Status = "running"
	sess.Phase = "parsing_callback"
	sess.Error = ""
	m.mu.Unlock()

	logCallbackSessionContext(rawURL, state, verifier, machineID)

	result, err := auth.ParseCallbackV2FromURL(rawURL)
	if err != nil {
		if auth.CallbackHasBinaryTokenString(rawURL) {
			if fallbackErr := m.completeViaLingmaLoginCallback(id, rawURL, nonce); fallbackErr == nil {
				return m.GetStatus(id), nil
			} else {
				err = fmt.Errorf("%w; lingma login callback fallback failed: %v", err, fallbackErr)
			}
		}
		m.updateSession(id, "error", fmt.Sprintf("parse callback url: %v", err))
		return m.GetStatus(id), err
	}
	if result.SecurityOAuthToken == "" {
		err = fmt.Errorf("callback missing security_oauth_token")
		m.updateSession(id, "error", err.Error())
		return m.GetStatus(id), err
	}

	stored, expireTime, err := m.buildStoredCredentialFromCallback(id, result, machineID)
	if err != nil {
		return m.GetStatus(id), err
	}

	m.updateSessionWithPhase(id, "running", "saving", "")
	if err := m.saveStoredCredential(context.Background(), stored); err != nil {
		m.updateSession(id, "error", fmt.Sprintf("save credentials: %v", err))
		return m.GetStatus(id), err
	}

	m.logAndReload(id, stored.Auth.UserID, result.AID, result.Name, stored.Auth.CosyKey, stored.Auth.MachineID, expireTime)
	return m.GetStatus(id), nil
}

func logCallbackSessionContext(rawURL, state, verifier, machineID string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("[callback-debug] session context parse_failed state_hash=%s verifier_hash=%s machine_hash=%s",
			shortHash(state), shortHash(verifier), shortHash(machineID))
		return
	}
	callbackState := parsed.Query().Get("state")
	log.Printf("[callback-debug] session context callback_state_len=%d session_state_len=%d state_match=%t callback_state_hash=%s session_state_hash=%s verifier_hash=%s machine_hash=%s",
		len(callbackState),
		len(state),
		callbackStateMatchesSessionState(callbackState, state),
		shortHash(callbackState),
		shortHash(state),
		shortHash(verifier),
		shortHash(machineID),
	)
}

func shortHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func callbackStateMatchesSessionState(callbackState, sessionState string) bool {
	if callbackState == "" || sessionState == "" {
		return false
	}
	if callbackState == sessionState {
		return true
	}
	return strings.TrimPrefix(sessionState, "2-") == callbackState
}

func queryValueFromPossiblyEscapedURL(rawURL, key string) string {
	current := rawURL
	for range 5 {
		parsed, err := url.Parse(current)
		if err == nil {
			if value := parsed.Query().Get(key); value != "" {
				return value
			}
		}
		decoded, err := url.QueryUnescape(current)
		if err != nil || decoded == current {
			break
		}
		current = decoded
	}
	return ""
}

func browserURLForGeneratedLingmaLoginURL(loginURL string) (string, error) {
	return auth.WrapLingmaLoginURLForBrowser(loginURL)
}

func lingmaCacheUserModTime() time.Time {
	path, err := lingmaCacheUserPath()
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func lingmaCacheUserPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".qoder-cn", "shared_client", "cache", "user"), nil
}

func (m *BootstrapManager) completeViaLingmaLoginCallback(id, rawURL, nonce string) error {
	authParam, tokenString, ok := auth.RawCallbackAuthTokenFromURL(rawURL)
	if !ok {
		return fmt.Errorf("extract raw auth/tokenString failed")
	}
	if nonce == "" {
		return fmt.Errorf("missing nonce for lingma login callback")
	}
	m.updateSessionWithPhase(id, "running", "lingma_auth_callback", "")
	log.Printf("[callback-debug] attempting Lingma login/auth_callback fallback nonce_hash=%s auth_len=%d tokenString_len=%d",
		shortHash(nonce), len(authParam), len(tokenString))

	lingmaRPC := m.lingmaLoginSession(id)
	var result auth.LingmaLoginCallbackResult
	var err error
	if lingmaRPC != nil {
		log.Printf("[callback-debug] using persistent Lingma login/auth_callback session nonce_hash=%s", shortHash(nonce))
		result, err = lingmaRPC.SubmitCallback(nonce, authParam, tokenString)
	} else {
		log.Printf("[callback-debug] using one-shot Lingma login/auth_callback session nonce_hash=%s", shortHash(nonce))
		result, err = submitLingmaLoginCallbackViaWebSocket(37010, 30*time.Second, nonce, authParam, tokenString)
	}
	if err != nil {
		log.Printf("[callback-debug] Lingma login/auth_callback fallback failed nonce_hash=%s err=%v",
			shortHash(nonce), err)
		return err
	}
	log.Printf("[callback-debug] Lingma login/auth_callback fallback returned success=%t error_code=%q",
		result.Success, result.ErrorCode)

	m.updateSessionWithPhase(id, "running", "importing_lingma_cache", "")
	log.Printf("[callback-debug] importing Lingma cache after auth_callback auth_file_len=%d", len(m.authFile))
	stored, err := m.importLingmaCacheCredential()
	if err != nil {
		log.Printf("[callback-debug] import Lingma cache failed err=%v", err)
		return err
	}
	log.Printf("[callback-debug] import Lingma cache ok user_hash=%s machine_hash=%s cosy_key_len=%d expire_len=%d",
		shortHash(stored.Auth.UserID), shortHash(stored.Auth.MachineID), len(stored.Auth.CosyKey), len(stored.TokenExpireTime))
	m.logAndReload(id, stored.Auth.UserID, "", "", stored.Auth.CosyKey, stored.Auth.MachineID, stored.TokenExpireTime)
	return nil
}

func (m *BootstrapManager) lingmaLoginSession(id string) *auth.LingmaLoginSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[id]; ok {
		return sess.lingmaRPC
	}
	return nil
}

func (m *BootstrapManager) lingmaLoginSessionContext(id string) (*auth.LingmaLoginSession, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[id]; ok {
		return sess.lingmaRPC, sess.cacheMTime
	}
	return nil, time.Time{}
}

func (m *BootstrapManager) sessionPKCEContext(id string) (state, verifier, nonce string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[id]; ok {
		return sess.state, sess.verifier, sess.nonce
	}
	return "", "", ""
}

func (m *BootstrapManager) buildStoredCredentialFromCallback(id string, result *auth.CallbackV2Result, machineID string) (proxy.StoredCredentialFile, string, error) {
	m.updateSessionWithPhase(id, "running", "generating_cosy", "")

	cosyKey, encryptUserInfo, err := auth.GenerateCosyCredentials(auth.CosyCredentialInput{
		Name:               result.Name,
		UID:                result.UID,
		AID:                result.AID,
		SecurityOAuthToken: result.SecurityOAuthToken,
		RefreshToken:       result.RefreshToken,
	})

	expireTime := ""
	if result.ExpireTime > 0 {
		expireTime = fmt.Sprintf("%d", result.ExpireTime)
	}

	if err == nil && cosyKey != "" && encryptUserInfo != "" {
		now := time.Now().Format(time.RFC3339)
		return proxy.StoredCredentialFile{
			SchemaVersion:     1,
			Source:            "oauth_v2_manual_callback",
			LingmaVersionHint: m.lingmaVer,
			ObtainedAt:        now,
			UpdatedAt:         now,
			TokenExpireTime:   expireTime,
			Auth: proxy.StoredAuthFields{
				CosyKey:         cosyKey,
				EncryptUserInfo: encryptUserInfo,
				UserID:          result.UID,
				MachineID:       machineID,
			},
			OAuth: proxy.StoredOAuthFields{
				AccessToken:  result.SecurityOAuthToken,
				RefreshToken: result.RefreshToken,
			},
		}, expireTime, nil
	}

	m.updateSessionWithPhase(id, "running", "deriving_remote", "")
	stored, remoteErr := auth.DeriveCredentialsRemotely(auth.RemoteLoginConfig{
		AccessToken:   result.SecurityOAuthToken,
		RefreshToken:  result.RefreshToken,
		UserID:        result.UID,
		Username:      result.Name,
		MachineID:     machineID,
		TokenExpireMs: expireTime,
	})
	if remoteErr != nil {
		m.updateSession(id, "error", fmt.Sprintf("derive credentials: %v", remoteErr))
		return proxy.StoredCredentialFile{}, "", remoteErr
	}
	if stored.Auth.UserID == "" {
		stored.Auth.UserID = result.UID
	}
	if stored.Auth.MachineID == "" {
		stored.Auth.MachineID = machineID
	}
	if stored.TokenExpireTime == "" {
		stored.TokenExpireTime = expireTime
	}
	return stored, stored.TokenExpireTime, nil
}

func (m *BootstrapManager) runRemoteCallbackFlow(ctx context.Context, id, machineID, listenAddr string) {
	defer func() {
		m.mu.Lock()
		if s, ok := m.sessions[id]; ok && s.cancel != nil {
			s.cancel = nil
		}
		m.mu.Unlock()
	}()

	if lingmaRPC, cacheMTime := m.lingmaLoginSessionContext(id); lingmaRPC != nil {
		m.runLingmaCacheImportFlow(ctx, id, cacheMTime)
		return
	}

	capture, err := auth.WaitForCallbackWithOptions(ctx, listenAddr, "/auth/callback", auth.WaitForCallbackOptions{
		AutoInjectHTML: true,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr == context.Canceled {
			m.updateSession(id, "cancelled", "")
			return
		}
		if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
			m.updateSession(id, "error", "timeout: user did not complete login within 5m")
			return
		}
		m.updateSession(id, "error", fmt.Sprintf("wait for callback: %v", err))
		return
	}

	if capture.Query.Get("auth") != "" && capture.Query.Get("token") != "" {
		rawURL := fmt.Sprintf("http://%s%s?%s", listenAddr, capture.Path, capture.Query.Encode())
		state, verifier, nonce := m.sessionPKCEContext(id)
		logCallbackSessionContext(rawURL, state, verifier, machineID)
		result, err := auth.ParseCallbackV2FromURL(rawURL)
		if err != nil {
			if auth.CallbackHasBinaryTokenString(rawURL) {
				if fallbackErr := m.completeViaLingmaLoginCallback(id, rawURL, nonce); fallbackErr == nil {
					return
				} else {
					err = fmt.Errorf("%w; lingma login callback fallback failed: %v", err, fallbackErr)
				}
			}
			m.updateSession(id, "error", fmt.Sprintf("parse callback url: %v", err))
			return
		}
		stored, _, err := m.buildStoredCredentialFromCallback(id, result, machineID)
		if err != nil {
			m.updateSession(id, "error", err.Error())
			return
		}
		if err := m.saveStoredCredential(ctx, stored); err != nil {
			m.updateSession(id, "error", fmt.Sprintf("save credentials: %v", err))
			return
		}
		m.logAndReload(id, stored.Auth.UserID, result.AID, result.Name, stored.Auth.CosyKey, machineID, stored.TokenExpireTime)
		return
	}

	if len(capture.Body) == 0 {
		m.updateSession(id, "error", fmt.Sprintf("callback did not contain user_info body (path=%s)", capture.Path))
		return
	}

	var submission struct {
		UserInfo string `json:"userInfo"`
		LoginURL string `json:"loginUrl"`
	}
	if err := json.Unmarshal(capture.Body, &submission); err != nil {
		m.updateSession(id, "error", fmt.Sprintf("parse user_info failed: %v", err))
		return
	}
	if submission.UserInfo == "" {
		m.updateSession(id, "error", "submit-userinfo body missing userInfo")
		return
	}

	payload, err := auth.ParseUserInfoJSON(submission.UserInfo)
	if err != nil {
		m.updateSession(id, "error", fmt.Sprintf("parse user_info: %v", err))
		return
	}

	result := &auth.CallbackV2Result{
		UID:                payload.UID,
		AID:                payload.AID,
		Name:               payload.Name,
		SecurityOAuthToken: payload.SecurityOauthToken,
		RefreshToken:       payload.RefreshToken,
		ExpireTime:         payload.ExpireTime,
	}
	stored, _, err := m.buildStoredCredentialFromCallback(id, result, machineID)
	if err != nil {
		m.updateSession(id, "error", err.Error())
		return
	}
	if err := m.saveStoredCredential(ctx, stored); err != nil {
		m.updateSession(id, "error", fmt.Sprintf("save credentials: %v", err))
		return
	}
	m.logAndReload(id, stored.Auth.UserID, result.AID, result.Name, stored.Auth.CosyKey, machineID, stored.TokenExpireTime)
}

func (m *BootstrapManager) runLingmaCacheImportFlow(ctx context.Context, id string, baseline time.Time) {
	m.updateSessionWithPhase(id, "running", "waiting_lingma_cache", "")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastLog := time.Time{}
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				m.updateSession(id, "cancelled", "")
				return
			}
			m.updateSession(id, "error", "timeout: user did not complete login within 5m")
			return
		case <-ticker.C:
		}

		path, pathErr := lingmaCacheUserPath()
		if pathErr != nil {
			m.updateSession(id, "error", fmt.Sprintf("find Lingma cache path: %v", pathErr))
			return
		}
		info, statErr := os.Stat(path)
		if statErr != nil || (!baseline.IsZero() && !info.ModTime().After(baseline)) {
			if time.Since(lastLog) >= 5*time.Second {
				if statErr != nil {
					log.Printf("[callback-debug] waiting Lingma cache update path=%s err=%v", path, statErr)
				} else {
					log.Printf("[callback-debug] waiting Lingma cache update path=%s baseline=%s current=%s",
						path, baseline.Format(time.RFC3339Nano), info.ModTime().Format(time.RFC3339Nano))
				}
				lastLog = time.Now()
			}
			continue
		}

		m.updateSessionWithPhase(id, "running", "importing_lingma_cache", "")
		log.Printf("[callback-debug] detected Lingma cache update path=%s baseline=%s current=%s",
			path, baseline.Format(time.RFC3339Nano), info.ModTime().Format(time.RFC3339Nano))
		stored, err := m.importLingmaCacheCredential()
		if err != nil {
			if time.Since(lastLog) >= 5*time.Second {
				log.Printf("[callback-debug] import Lingma cache after update failed err=%v", err)
				lastLog = time.Now()
			}
			m.updateSessionWithPhase(id, "running", "waiting_lingma_cache", "")
			continue
		}
		log.Printf("[callback-debug] import Lingma cache ok user_hash=%s machine_hash=%s cosy_key_len=%d expire_len=%d",
			shortHash(stored.Auth.UserID), shortHash(stored.Auth.MachineID), len(stored.Auth.CosyKey), len(stored.TokenExpireTime))
		m.logAndReload(id, stored.Auth.UserID, "", "", stored.Auth.CosyKey, stored.Auth.MachineID, stored.TokenExpireTime)
		return
	}
}

func (m *BootstrapManager) saveStoredCredential(ctx context.Context, stored proxy.StoredCredentialFile) error {
	if m.Accounts != nil {
		return m.Accounts.UpsertAccount(ctx, storedFileToChinaAccount(stored))
	}
	return auth.SaveCredentialFile(m.authFile, stored)
}

func (m *BootstrapManager) importLingmaCacheCredential() (proxy.StoredCredentialFile, error) {
	if m.Accounts == nil {
		return tryImportFromLingmaCache(m.authFile)
	}

	tempFile, err := os.CreateTemp("", "lingma-cache-import-*.json")
	if err != nil {
		return proxy.StoredCredentialFile{}, fmt.Errorf("create temporary import file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return proxy.StoredCredentialFile{}, fmt.Errorf("close temporary import file: %w", err)
	}
	defer os.Remove(tempPath)

	stored, err := tryImportFromLingmaCache(tempPath)
	if err != nil {
		return proxy.StoredCredentialFile{}, err
	}
	if err := m.saveStoredCredential(context.Background(), stored); err != nil {
		return proxy.StoredCredentialFile{}, err
	}
	return stored, nil
}

func storedFileToChinaAccount(stored proxy.StoredCredentialFile) proxy.StoredCredentialAccount {
	return proxy.StoredCredentialAccount{
		ID:                generatedBootstrapAccountID(stored.Auth.UserID, stored.Auth.MachineID),
		Label:             "China account",
		Region:            proxy.AccountRegionChina,
		Enabled:           true,
		Source:            stored.Source,
		LingmaVersionHint: stored.LingmaVersionHint,
		ObtainedAt:        stored.ObtainedAt,
		UpdatedAt:         stored.UpdatedAt,
		TokenExpireTime:   stored.TokenExpireTime,
		Auth:              stored.Auth,
		OAuth:             stored.OAuth,
	}
}

func generatedBootstrapAccountID(userID, machineID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{string(proxy.AccountRegionChina), userID, machineID}, "\x00")))
	return "acct-" + hex.EncodeToString(sum[:8])
}

func portFromAddr(addr string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", fmt.Errorf("invalid addr %q", addr)
	}
	return port, nil
}

func resolveListenAddr(configured string) (string, error) {
	if configured == "" {
		configured = "127.0.0.1:37510"
	}
	l, err := net.Listen("tcp", configured)
	if err == nil {
		l.Close()
		return configured, nil
	}
	l, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("no free port available: %w", err)
	}
	defer l.Close()
	return l.Addr().String(), nil
}
