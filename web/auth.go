package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled      bool   `json:"enabled"`
	Username     string `json:"username"`
	Password     string `json:"password"`      // Deprecated: use PasswordHash instead
	PasswordHash string `json:"password_hash"` // bcrypt hash of password (preferred)
	SecretKey    string `json:"secret_key"`
	TokenExpiry  int    `json:"token_expiry"` // in hours
}

// Simple JWT implementation (for demonstration - in production use a proper JWT library)
type TokenManager struct {
	secretKey   []byte
	tokens      map[string]*TokenData
	mu          sync.RWMutex
	tokenExpiry time.Duration
}

type TokenData struct {
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewTokenManager(secretKey string, expiryHours int) *TokenManager {
	if secretKey == "" {
		// Generate a random key if not provided
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		secretKey = base64.StdEncoding.EncodeToString(key)
	}

	return &TokenManager{
		secretKey:   []byte(secretKey),
		tokens:      make(map[string]*TokenData),
		tokenExpiry: time.Duration(expiryHours) * time.Hour,
	}
}

// GenerateToken creates a new authentication token
func (tm *TokenManager) GenerateToken(username string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Generate random token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(b)

	// Store token data
	tm.tokens[token] = &TokenData{
		Username:  username,
		ExpiresAt: time.Now().Add(tm.tokenExpiry),
	}

	// Clean up expired tokens periodically
	go tm.cleanupExpiredTokens()

	return token, nil
}

// ValidateToken checks if a token is valid
func (tm *TokenManager) ValidateToken(token string) (*TokenData, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	data, exists := tm.tokens[token]
	if !exists {
		return nil, false
	}

	if time.Now().After(data.ExpiresAt) {
		return nil, false
	}

	return data, true
}

// RevokeToken invalidates a token
func (tm *TokenManager) RevokeToken(token string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.tokens, token)
}

// cleanupExpiredTokens removes expired tokens from memory
func (tm *TokenManager) cleanupExpiredTokens() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	for token, data := range tm.tokens {
		if now.After(data.ExpiresAt) {
			delete(tm.tokens, token)
		}
	}
}

// authMiddleware checks authentication for protected endpoints
func authMiddleware(tm *TokenManager, required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public endpoints
			if !required {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				// Check cookie as fallback
				cookie, err := r.Cookie("auth_token")
				if err != nil {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				authHeader = "Bearer " + cookie.Value
			}

			// Validate Bearer token
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			// Validate token
			tokenData, valid := tm.ValidateToken(parts[1])
			if !valid {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Add username to request context
			r.Header.Set("X-Auth-User", tokenData.Username)

			next.ServeHTTP(w, r)
		})
	}
}

// LoginHandler handles authentication requests
type LoginHandler struct {
	config       *AuthConfig
	tokenManager *TokenManager
}

func NewLoginHandler(config *AuthConfig, tm *TokenManager) *LoginHandler {
	return &LoginHandler{
		config:       config,
		tokenManager: tm,
	}
}

func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate credentials using secure comparison
	// This is legacy auth - use JWT for production
	usernameMatch := subtle.ConstantTimeCompare([]byte(credentials.Username), []byte(h.config.Username)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(credentials.Password), []byte(h.config.Password)) == 1

	if !usernameMatch || !passwordMatch {
		// Add slight delay to prevent timing attacks
		time.Sleep(100 * time.Millisecond)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate token
	token, err := h.tokenManager.GenerateToken(credentials.Username)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Set cookie for web UI
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.tokenManager.tokenExpiry.Seconds()),
	})

	// Return token in response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"expires_in": h.tokenManager.tokenExpiry.Seconds(),
	})
}

// LogoutHandler handles logout requests
type LogoutHandler struct {
	tokenManager *TokenManager
}

func NewLogoutHandler(tm *TokenManager) *LogoutHandler {
	return &LogoutHandler{tokenManager: tm}
}

func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get token from header or cookie
	var token string
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			token = parts[1]
		}
	} else {
		cookie, err := r.Cookie("auth_token")
		if err == nil {
			token = cookie.Value
		}
	}

	// Revoke token if found
	if token != "" {
		h.tokenManager.RevokeToken(token)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Logged out successfully")
}
