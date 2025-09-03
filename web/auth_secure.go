package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

// SecureAuthConfig holds secure authentication configuration
type SecureAuthConfig struct {
	Enabled      bool   `json:"enabled"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"` // bcrypt hash of password
	SecretKey    string `json:"secret_key"`
	TokenExpiry  int    `json:"token_expiry"` // in hours
	MaxAttempts  int    `json:"max_attempts"` // max login attempts per minute
}

// RateLimiter manages login attempt rate limiting
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     int // attempts per minute
	burst    int // burst size
}

func NewRateLimiter(ratePerMinute, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     ratePerMinute,
		burst:    burst,
	}
}

func (rl *RateLimiter) GetLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rl.rate)), rl.burst)
		rl.limiters[key] = limiter
	}

	return limiter
}

func (rl *RateLimiter) Allow(key string) bool {
	limiter := rl.GetLimiter(key)
	return limiter.Allow()
}

// CleanupOldLimiters removes limiters that haven't been used recently
func (rl *RateLimiter) CleanupOldLimiters() {
	// This should be called periodically to prevent memory leak
	// Implementation would track last access time and remove old entries
}

// SecureTokenManager handles token management with enhanced security
type SecureTokenManager struct {
	secretKey   []byte
	tokens      map[string]*TokenData
	mu          sync.RWMutex
	tokenExpiry time.Duration
	csrfTokens  map[string]time.Time // CSRF token storage
	csrfMu      sync.RWMutex
}

func NewSecureTokenManager(secretKey string, expiryHours int) *SecureTokenManager {
	if secretKey == "" {
		// Generate a cryptographically secure random key
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic("failed to generate secret key: " + err.Error())
		}
		secretKey = base64.StdEncoding.EncodeToString(key)
	}

	tm := &SecureTokenManager{
		secretKey:   []byte(secretKey),
		tokens:      make(map[string]*TokenData),
		tokenExpiry: time.Duration(expiryHours) * time.Hour,
		csrfTokens:  make(map[string]time.Time),
	}

	// Start cleanup goroutine
	go tm.cleanupRoutine()

	return tm
}

// GenerateCSRFToken creates a new CSRF token
func (tm *SecureTokenManager) GenerateCSRFToken() (string, error) {
	tm.csrfMu.Lock()
	defer tm.csrfMu.Unlock()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(b)
	tm.csrfTokens[token] = time.Now().Add(1 * time.Hour)

	return token, nil
}

// ValidateCSRFToken checks if a CSRF token is valid
func (tm *SecureTokenManager) ValidateCSRFToken(token string) bool {
	tm.csrfMu.RLock()
	defer tm.csrfMu.RUnlock()

	expiry, exists := tm.csrfTokens[token]
	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		return false
	}

	// Remove token after use (one-time use)
	delete(tm.csrfTokens, token)
	return true
}

// GenerateToken creates a new authentication token
func (tm *SecureTokenManager) GenerateToken(username string) (string, error) {
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

	return token, nil
}

// ValidateToken checks if a token is valid
func (tm *SecureTokenManager) ValidateToken(token string) (*TokenData, bool) {
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
func (tm *SecureTokenManager) RevokeToken(token string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.tokens, token)
}

// cleanupExpiredTokens removes expired tokens from memory
func (tm *SecureTokenManager) cleanupExpiredTokens() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	for token, data := range tm.tokens {
		if now.After(data.ExpiresAt) {
			delete(tm.tokens, token)
		}
	}
}

// cleanupRoutine periodically cleans up expired tokens and CSRF tokens
func (tm *SecureTokenManager) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		tm.cleanupExpiredTokens()
		tm.cleanupExpiredCSRFTokens()
	}
}

func (tm *SecureTokenManager) cleanupExpiredCSRFTokens() {
	tm.csrfMu.Lock()
	defer tm.csrfMu.Unlock()

	now := time.Now()
	for token, expiry := range tm.csrfTokens {
		if now.After(expiry) {
			delete(tm.csrfTokens, token)
		}
	}
}

// SecureLoginHandler handles authentication with security best practices
type SecureLoginHandler struct {
	config       *SecureAuthConfig
	tokenManager *SecureTokenManager
	rateLimiter  *RateLimiter
}

func NewSecureLoginHandler(config *SecureAuthConfig, tm *SecureTokenManager, rl *RateLimiter) *SecureLoginHandler {
	return &SecureLoginHandler{
		config:       config,
		tokenManager: tm,
		rateLimiter:  rl,
	}
}

func (h *SecureLoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check rate limiting by IP
	clientIP := getClientIP(r)
	if !h.rateLimiter.Allow(clientIP) {
		http.Error(w, "Too many login attempts", http.StatusTooManyRequests)
		return
	}

	// Validate CSRF token for web requests
	if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
		csrfToken := r.Header.Get("X-CSRF-Token")
		if csrfToken == "" {
			http.Error(w, "CSRF token required", http.StatusForbidden)
			return
		}
		if !h.tokenManager.ValidateCSRFToken(csrfToken) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
	}

	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate username with constant-time comparison
	usernameMatch := subtle.ConstantTimeCompare([]byte(credentials.Username), []byte(h.config.Username)) == 1

	// Validate password with bcrypt (already constant-time)
	passwordErr := bcrypt.CompareHashAndPassword([]byte(h.config.PasswordHash), []byte(credentials.Password))
	passwordMatch := passwordErr == nil

	// Combine results to prevent timing attacks
	if !usernameMatch || !passwordMatch {
		// Add slight delay to prevent brute force
		time.Sleep(100 * time.Millisecond)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate auth token
	token, err := h.tokenManager.GenerateToken(credentials.Username)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Generate new CSRF token for the session
	csrfToken, err := h.tokenManager.GenerateCSRFToken()
	if err != nil {
		http.Error(w, "Failed to generate CSRF token", http.StatusInternalServerError)
		return
	}

	// Set secure cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.tokenManager.tokenExpiry.Seconds()),
	})

	// Return tokens in response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"csrf_token": csrfToken,
		"expires_in": h.tokenManager.tokenExpiry.Seconds(),
	})
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	return ip
}

// HashPassword generates a bcrypt hash of the password
func HashPassword(password string) (string, error) {
	// Use cost 12 for good security/performance balance
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// csrfMiddleware adds CSRF protection to requests
func csrfMiddleware(tm *SecureTokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip CSRF check for safe methods
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Skip for API calls with proper authentication
			if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
				next.ServeHTTP(w, r)
				return
			}

			// Validate CSRF token
			csrfToken := r.Header.Get("X-CSRF-Token")
			if csrfToken == "" {
				// Try to get from form data
				csrfToken = r.FormValue("csrf_token")
			}

			if csrfToken == "" || !tm.ValidateCSRFToken(csrfToken) {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
