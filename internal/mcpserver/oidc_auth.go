package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// OIDCAuthMiddleware provides OpenID Connect authentication for MCP server
type OIDCAuthMiddleware struct {
	provider      *oidc.Provider
	oauth2Config  oauth2.Config
	verifier      *oidc.IDTokenVerifier
	tokenStore    *TokenStore
	callbackPort  int
	enableLogging bool
	customConfig  *OIDCConfig
	mutex         sync.RWMutex

	// Integrated callback support
	pendingMutex sync.Mutex
	pendingFlows map[string]*authFlow
}

// OIDCConfig contains configuration for OIDC authentication
type OIDCConfig struct {
	Issuer        string   // OIDC provider URL (e.g., "https://accounts.google.com")
	ClientID      string   // OAuth2 client ID
	ClientSecret  string   // OAuth2 client secret
	RedirectURL   string   // Callback URL (optional, will use dynamic port if empty)
	Scopes        []string // OAuth2 scopes (defaults to ["openid", "profile", "email"])
	CallbackPort  int      // Port for callback listener (0 for random port)
	EnableLogging bool     // Enable detailed logging

	// Custom endpoint URLs (optional - if not set, will use OIDC discovery)
	AuthorizationURL string // Custom authorization endpoint URL
	TokenURL         string // Custom token endpoint URL
	UserInfoURL      string // Custom userinfo endpoint URL
	JWKSURL          string // Custom JWKS endpoint URL for token validation

	// Skip OIDC discovery and use only custom endpoints
	SkipDiscovery bool // If true, skip OIDC discovery and use custom endpoints only
}

// TokenStore manages authentication tokens
type TokenStore struct {
	tokens map[string]*TokenInfo
	mutex  sync.RWMutex
}

// TokenInfo contains token information
type TokenInfo struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
	Claims       map[string]interface{}
	ExpiresAt    time.Time
	Email        string
	Subject      string
}

// authFlow tracks an in-flight OAuth2 authorization flow keyed by state
type authFlow struct {
	tokenChan   chan *TokenInfo
	errorChan   chan error
	redirectURL string
}

// NewOIDCAuthMiddleware creates a new OIDC authentication middleware
func NewOIDCAuthMiddleware(config *OIDCConfig) (*OIDCAuthMiddleware, error) {
	if config.ClientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}

	// Default scopes if not provided
	if len(config.Scopes) == 0 {
		config.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	// Determine redirect URL
	redirectURL := config.RedirectURL
	if redirectURL == "" {
		port := config.CallbackPort
		if port == 0 {
			// Align default with MCP_SERVER_PORT default (8080)
			port = 8080
		}
		redirectURL = fmt.Sprintf("http://localhost:%d/callback", port)
	}

	ctx := context.Background()
	var provider *oidc.Provider
	var oauth2Config oauth2.Config
	var verifier *oidc.IDTokenVerifier

	// Check if we should use custom endpoints or OIDC discovery
	if config.SkipDiscovery || (config.AuthorizationURL != "" && config.TokenURL != "") {
		// Use custom endpoints without discovery
		if config.AuthorizationURL == "" || config.TokenURL == "" {
			return nil, fmt.Errorf("authorization URL and token URL are required when using custom endpoints")
		}

		// Create OAuth2 config with custom endpoints
		oauth2Config = oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  config.AuthorizationURL,
				TokenURL: config.TokenURL,
			},
			RedirectURL: redirectURL,
			Scopes:      config.Scopes,
		}

		// If JWKS URL is provided, we can still create a provider for token validation
		if config.JWKSURL != "" && config.Issuer != "" {
			// Try to create provider for token validation only
			provider, _ = oidc.NewProvider(ctx, config.Issuer)
			if provider != nil {
				verifier = provider.Verifier(&oidc.Config{
					ClientID: config.ClientID,
				})
			}
		}

		if config.EnableLogging {
			log.Printf("Using custom OIDC endpoints - Auth: %s, Token: %s",
				config.AuthorizationURL, config.TokenURL)
		}
	} else {
		// Use OIDC discovery
		if config.Issuer == "" {
			return nil, fmt.Errorf("issuer URL is required for OIDC discovery")
		}

		// Create OIDC provider with discovery
		var err error
		provider, err = oidc.NewProvider(ctx, config.Issuer)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
		}

		// Get endpoint from provider
		endpoint := provider.Endpoint()

		// Override with custom URLs if provided
		if config.AuthorizationURL != "" {
			endpoint.AuthURL = config.AuthorizationURL
			if config.EnableLogging {
				log.Printf("Overriding authorization URL: %s", config.AuthorizationURL)
			}
		}
		if config.TokenURL != "" {
			endpoint.TokenURL = config.TokenURL
			if config.EnableLogging {
				log.Printf("Overriding token URL: %s", config.TokenURL)
			}
		}

		// Create OAuth2 config
		oauth2Config = oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     endpoint,
			RedirectURL:  redirectURL,
			Scopes:       config.Scopes,
		}

		// Create ID token verifier
		verifier = provider.Verifier(&oidc.Config{
			ClientID: config.ClientID,
		})
	}

	middleware := &OIDCAuthMiddleware{
		provider:      provider,
		oauth2Config:  oauth2Config,
		verifier:      verifier,
		tokenStore:    NewTokenStore(),
		callbackPort:  config.CallbackPort,
		enableLogging: config.EnableLogging,
		customConfig:  config,
		pendingFlows:  make(map[string]*authFlow),
	}

	if middleware.enableLogging {
		if config.SkipDiscovery {
			log.Printf("OIDC Auth Middleware initialized with custom endpoints")
		} else {
			log.Printf("OIDC Auth Middleware initialized with issuer: %s", config.Issuer)
		}
	}

	return middleware, nil
}

// NewTokenStore creates a new token store
func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokens: make(map[string]*TokenInfo),
	}
}

// Middleware returns the HTTP middleware function
func (m *OIDCAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow OAuth2 callback path to bypass auth checks
		if r.URL.Path == "/callback" {
			next.ServeHTTP(w, r)
			return
		}
		// Extract token from request
		token := m.extractToken(r)
		if token == "" {
			if m.enableLogging {
				log.Printf("No authentication token found in request")
			}
			m.sendAuthenticationRequired(w, r)
			return
		}

		// Validate token
		tokenInfo, err := m.validateToken(token)
		if err != nil {
			if m.enableLogging {
				log.Printf("Token validation failed: %v", err)
			}
			m.sendAuthenticationRequired(w, r)
			return
		}

		// Check if token is expired
		if time.Now().After(tokenInfo.ExpiresAt) {
			if m.enableLogging {
				log.Printf("Token expired for user: %s", tokenInfo.Email)
			}
			m.sendAuthenticationRequired(w, r)
			return
		}

		if m.enableLogging {
			log.Printf("Authentication successful for user: %s", tokenInfo.Email)
		}

		// Add user info to request context
		ctx := context.WithValue(r.Context(), userContextKey, tokenInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractToken extracts the authentication token from the request
func (m *OIDCAuthMiddleware) extractToken(r *http.Request) string {
	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if len(authHeader) > len(bearerPrefix) && authHeader[:len(bearerPrefix)] == bearerPrefix {
			return authHeader[len(bearerPrefix):]
		}
	}

	// Check query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	// Check cookie
	if cookie, err := r.Cookie("mcp_auth_token"); err == nil {
		return cookie.Value
	}

	return ""
}

// validateToken validates the ID token
func (m *OIDCAuthMiddleware) validateToken(tokenString string) (*TokenInfo, error) {
	// Check token store first
	m.tokenStore.mutex.RLock()
	if info, exists := m.tokenStore.tokens[tokenString]; exists {
		m.tokenStore.mutex.RUnlock()
		if time.Now().Before(info.ExpiresAt) {
			return info, nil
		}
		// Token expired, remove from store
		m.tokenStore.mutex.Lock()
		delete(m.tokenStore.tokens, tokenString)
		m.tokenStore.mutex.Unlock()
	} else {
		m.tokenStore.mutex.RUnlock()
	}

	// If we have a verifier, use it for proper validation
	if m.verifier != nil {
		ctx := context.Background()
		idToken, err := m.verifier.Verify(ctx, tokenString)
		if err != nil {
			return nil, fmt.Errorf("failed to verify ID token: %w", err)
		}

		// Extract claims
		var claims map[string]interface{}
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("failed to extract claims: %w", err)
		}

		// Create token info
		tokenInfo := &TokenInfo{
			IDToken:   tokenString,
			Claims:    claims,
			ExpiresAt: idToken.Expiry,
			Subject:   idToken.Subject,
		}

		// Extract email if available
		if email, ok := claims["email"].(string); ok {
			tokenInfo.Email = email
		}

		// Store token
		m.tokenStore.mutex.Lock()
		m.tokenStore.tokens[tokenString] = tokenInfo
		m.tokenStore.mutex.Unlock()

		return tokenInfo, nil
	}

	// If no verifier available (custom endpoints without JWKS), do basic JWT parsing
	// This is less secure but allows for custom providers
	if m.customConfig != nil && m.customConfig.SkipDiscovery {
		return m.validateTokenWithoutVerifier(tokenString)
	}

	return nil, fmt.Errorf("no token verifier available")
}

// validateTokenWithoutVerifier performs basic JWT validation without signature verification
func (m *OIDCAuthMiddleware) validateTokenWithoutVerifier(tokenString string) (*TokenInfo, error) {
	// Parse JWT without verification (less secure, use only when necessary)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to extract JWT claims")
	}

	// Extract standard claims
	tokenInfo := &TokenInfo{
		IDToken: tokenString,
		Claims:  claims,
	}

	// Extract expiration
	if exp, ok := claims["exp"].(float64); ok {
		tokenInfo.ExpiresAt = time.Unix(int64(exp), 0)
		// Check if token is expired
		if time.Now().After(tokenInfo.ExpiresAt) {
			return nil, fmt.Errorf("token is expired")
		}
	}

	// Extract subject
	if sub, ok := claims["sub"].(string); ok {
		tokenInfo.Subject = sub
	}

	// Extract email
	if email, ok := claims["email"].(string); ok {
		tokenInfo.Email = email
	}

	// Store token
	m.tokenStore.mutex.Lock()
	m.tokenStore.tokens[tokenString] = tokenInfo
	m.tokenStore.mutex.Unlock()

	if m.enableLogging {
		log.Printf("WARNING: Token validated without signature verification (custom endpoints mode)")
	}

	return tokenInfo, nil
}

// sendAuthenticationRequired sends an authentication required response
func (m *OIDCAuthMiddleware) sendAuthenticationRequired(w http.ResponseWriter, r *http.Request) {
	// Return JSON-RPC error for API calls
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	// Build auth URL bound to request host
	authURL := m.GetAuthURLForRequest(r)
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    -32001,
			"message": "Authentication required",
			"data": map[string]interface{}{
				"auth_url": authURL,
			},
		},
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

// GetAuthURL generates the authentication URL
func (m *OIDCAuthMiddleware) GetAuthURL() string {
	state := m.generateState()
	// Track pending flow for integrated callback handling
	m.pendingMutex.Lock()
	m.pendingFlows[state] = &authFlow{
		tokenChan: nil,
		errorChan: nil,
	}
	m.pendingMutex.Unlock()

	return m.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// GetAuthURLForRequest generates an auth URL using the request's host/scheme
func (m *OIDCAuthMiddleware) GetAuthURLForRequest(r *http.Request) string {
	state := m.generateState()
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	} else if xfProto := r.Header.Get("X-Forwarded-Proto"); xfProto != "" {
		scheme = xfProto
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	redirect := fmt.Sprintf("%s://%s/callback", scheme, host)

	m.mutex.Lock()
	m.oauth2Config.RedirectURL = redirect
	m.mutex.Unlock()

	m.pendingMutex.Lock()
	m.pendingFlows[state] = &authFlow{redirectURL: redirect}
	m.pendingMutex.Unlock()

	return m.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// StartAuthFlow initiates the authentication flow
func (m *OIDCAuthMiddleware) StartAuthFlow(ctx context.Context) (*TokenInfo, error) {
	// Generate state for CSRF protection
	state := m.generateState()

	// Prepare channels for this flow and register
	callbackChan := make(chan *TokenInfo, 1)
	errorChan := make(chan error, 1)
	m.pendingMutex.Lock()
	m.pendingFlows[state] = &authFlow{tokenChan: callbackChan, errorChan: errorChan}
	m.pendingMutex.Unlock()
	defer func() {
		// Ensure cleanup in case of timeout/cancellation
		m.pendingMutex.Lock()
		delete(m.pendingFlows, state)
		m.pendingMutex.Unlock()
	}()

	// Generate auth URL using pre-configured RedirectURL (MCP server port)
	authURL := m.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	if m.enableLogging {
		log.Printf("Starting authentication flow with URL: %s", authURL)
	}

	// Open browser
	if err := openBrowser(authURL); err != nil {
		log.Printf("Failed to open browser: %v. Please visit: %s", err, authURL)
	}

	// Wait for callback via integrated handler
	select {
	case tokenInfo := <-callbackChan:
		return tokenInfo, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timeout")
	}
}

// HandleCallback processes the OAuth2 redirect on the same MCP server port
func (m *OIDCAuthMiddleware) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Extract state and code
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "Missing state", http.StatusBadRequest)
		return
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		m.notifyPendingFlowError(state, fmt.Errorf("authentication error: %s", errMsg))
		http.Error(w, "Authentication failed", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		m.notifyPendingFlowError(state, fmt.Errorf("no authorization code received"))
		http.Error(w, "No authorization code", http.StatusBadRequest)
		return
	}

	// Ensure redirect URL matches the one used to start auth (per provider requirement)
	m.pendingMutex.Lock()
	if flow, ok := m.pendingFlows[state]; ok && flow.redirectURL != "" {
		m.mutex.Lock()
		m.oauth2Config.RedirectURL = flow.redirectURL
		m.mutex.Unlock()
	}
	m.pendingMutex.Unlock()

	// Exchange authorization code for tokens
	ctx := context.Background()
	token, err := m.oauth2Config.Exchange(ctx, code)
	if err != nil {
		m.notifyPendingFlowError(state, fmt.Errorf("failed to exchange code: %w", err))
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		m.notifyPendingFlowError(state, fmt.Errorf("no ID token in response"))
		http.Error(w, "No ID token", http.StatusInternalServerError)
		return
	}

	var tokenInfo *TokenInfo
	if m.verifier == nil {
		// Fallback when verifier is unavailable (custom endpoints without JWKS)
		info, err := m.validateTokenWithoutVerifier(rawIDToken)
		if err != nil {
			m.notifyPendingFlowError(state, fmt.Errorf("failed to parse ID token: %w", err))
			http.Error(w, "Token parsing failed", http.StatusInternalServerError)
			return
		}
		// Attach access/refresh tokens and expiry
		info.AccessToken = token.AccessToken
		info.RefreshToken = token.RefreshToken
		info.ExpiresAt = token.Expiry
		tokenInfo = info
	} else {
		// Verify ID token
		idToken, err := m.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			m.notifyPendingFlowError(state, fmt.Errorf("failed to verify ID token: %w", err))
			http.Error(w, "Token verification failed", http.StatusInternalServerError)
			return
		}

		// Extract claims
		var claims map[string]interface{}
		if err := idToken.Claims(&claims); err != nil {
			m.notifyPendingFlowError(state, fmt.Errorf("failed to extract claims: %w", err))
			http.Error(w, "Claims extraction failed", http.StatusInternalServerError)
			return
		}

		// Create token info
		tokenInfo = &TokenInfo{
			IDToken:      rawIDToken,
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			Claims:       claims,
			ExpiresAt:    token.Expiry,
			Subject:      idToken.Subject,
		}
		if email, ok := claims["email"].(string); ok {
			tokenInfo.Email = email
		}
	}

	// Store token
	m.tokenStore.mutex.Lock()
	m.tokenStore.tokens[rawIDToken] = tokenInfo
	m.tokenStore.mutex.Unlock()

	// Build MCP add command suggestion
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	} else if xfProto := r.Header.Get("X-Forwarded-Proto"); xfProto != "" {
		scheme = xfProto
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, host)
	addCmd := fmt.Sprintf("claude mcp add --transport sse ragent %s/sse --header \"Authorization: Bearer %s\"", serverURL, rawIDToken)

	// Optionally set a cookie for browser-based calls
	http.SetCookie(w, &http.Cookie{Name: "mcp_auth_token", Value: rawIDToken, Path: "/", HttpOnly: true, Secure: scheme == "https"})

	// Success response with instruction (English first, then Japanese)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := fmt.Fprintf(w, `
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="utf-8" />
            <title>Authentication Successful</title>
            <style>
                body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; margin: 40px; line-height: 1.6; }
                code, pre { background: #f5f7fa; border: 1px solid #e3e8ef; border-radius: 6px; padding: 12px; display: block; white-space: pre-wrap; word-break: break-all; }
                .small { color: #6b7280; font-size: 12px; }
                hr { border: none; border-top: 1px solid #e5e7eb; margin: 24px 0; }
            </style>
        </head>
        <body>
            <h1>Authentication Successful</h1>
            <p>Run the following command to add this MCP server to Claude.</p>
            <pre><code>%s</code></pre>
            <p class="small">This token is sensitive. Do not share it with anyone.</p>

            <hr />

            <h1>認証に成功しました</h1>
            <p>以下のコマンドを実行して、Claude にこの MCP サーバーを追加してください。</p>
            <pre><code>%s</code></pre>
            <p class="small">このトークンは機密情報です。第三者に共有しないでください。</p>
        </body>
        </html>
    `, addCmd, addCmd); err != nil {
		log.Printf("Failed to write HTML response: %v", err)
	}

	// Notify pending flow if present
	m.notifyPendingFlowSuccess(state, tokenInfo)
}

func (m *OIDCAuthMiddleware) notifyPendingFlowSuccess(state string, token *TokenInfo) {
	m.pendingMutex.Lock()
	defer m.pendingMutex.Unlock()
	if flow, ok := m.pendingFlows[state]; ok {
		if flow.tokenChan != nil {
			flow.tokenChan <- token
		}
		delete(m.pendingFlows, state)
	}
}

func (m *OIDCAuthMiddleware) notifyPendingFlowError(state string, err error) {
	m.pendingMutex.Lock()
	defer m.pendingMutex.Unlock()
	if flow, ok := m.pendingFlows[state]; ok {
		if flow.errorChan != nil {
			flow.errorChan <- err
		}
		delete(m.pendingFlows, state)
	}
}

// startCallbackServer starts the OAuth2 callback server
// generateState generates a random state string for CSRF protection
func (m *OIDCAuthMiddleware) generateState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("Failed to generate random bytes: %v", err)
	}
	return base64.URLEncoding.EncodeToString(b)
}

// ValidateJWT validates a JWT token without OIDC provider verification
func (m *OIDCAuthMiddleware) ValidateJWT(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// This would need proper JWKS handling in production
		// For now, we'll rely on the OIDC verifier
		return nil, fmt.Errorf("JWT validation requires OIDC verifier")
	})

	if err != nil {
		return nil, err
	}

	return token, nil
}

// GetUserInfo retrieves user information from the provider
func (m *OIDCAuthMiddleware) GetUserInfo(ctx context.Context, accessToken string) (map[string]interface{}, error) {
	// Check if custom UserInfo URL is configured
	if m.customConfig != nil && m.customConfig.UserInfoURL != "" {
		return m.getUserInfoFromCustomEndpoint(ctx, accessToken, m.customConfig.UserInfoURL)
	}

	// If no provider is available (custom endpoints without discovery), return error
	if m.provider == nil {
		return nil, fmt.Errorf("user info endpoint not available")
	}

	// Use the OIDC UserInfo method
	userInfo, err := m.provider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: accessToken,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Extract claims
	var claims map[string]interface{}
	if err := userInfo.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract user info claims: %w", err)
	}

	return claims, nil
}

// getUserInfoFromCustomEndpoint retrieves user info from a custom endpoint
func (m *OIDCAuthMiddleware) getUserInfoFromCustomEndpoint(ctx context.Context, accessToken, userInfoURL string) (map[string]interface{}, error) {
	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status: %s", resp.Status)
	}

	// Parse response
	var userInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode user info response: %w", err)
	}

	return userInfo, nil
}

// RevokeToken revokes an access or refresh token
func (m *OIDCAuthMiddleware) RevokeToken(ctx context.Context, token string) error {
	// Note: Token revocation endpoint is not part of standard OIDC discovery
	// Many providers support it as an extension
	// For now, we'll just remove from local store

	// Remove from token store
	m.tokenStore.mutex.Lock()
	delete(m.tokenStore.tokens, token)
	m.tokenStore.mutex.Unlock()

	if m.enableLogging {
		log.Printf("Token revoked and removed from store")
	}

	return nil
}

// ClearTokenStore clears all stored tokens
func (m *OIDCAuthMiddleware) ClearTokenStore() {
	m.tokenStore.mutex.Lock()
	defer m.tokenStore.mutex.Unlock()
	m.tokenStore.tokens = make(map[string]*TokenInfo)
}

// GetStoredTokenCount returns the number of stored tokens
func (m *OIDCAuthMiddleware) GetStoredTokenCount() int {
	m.tokenStore.mutex.RLock()
	defer m.tokenStore.mutex.RUnlock()
	return len(m.tokenStore.tokens)
}

// openBrowser opens the default web browser to the specified URL
func openBrowser(url string) error {
	err := openBrowserCommand(url)
	if err != nil {
		log.Printf("Failed to open browser automatically: %v", err)
		log.Printf("Please open the following URL in your browser: %s", url)
	}
	return err
}
