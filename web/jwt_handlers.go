package web

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// JWTLoginHandler handles login requests with JWT
type JWTLoginHandler struct {
	config     *AuthConfig
	jwtManager *JWTManager
}

// NewJWTLoginHandler creates a new JWT login handler
func NewJWTLoginHandler(config *AuthConfig, jm *JWTManager) *JWTLoginHandler {
	return &JWTLoginHandler{
		config:     config,
		jwtManager: jm,
	}
}

// LoginRequest represents the login request body
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents the login response
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Username  string    `json:"username"`
}

// ServeHTTP handles the login request
func (h *JWTLoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate credentials using secure comparison
	// Use bcrypt if password hash is available, otherwise fallback to plain text for migration
	usernameMatch := subtle.ConstantTimeCompare([]byte(req.Username), []byte(h.config.Username)) == 1

	var passwordMatch bool
	if h.config.PasswordHash != "" {
		// Use bcrypt comparison for hashed passwords
		err := bcrypt.CompareHashAndPassword([]byte(h.config.PasswordHash), []byte(req.Password))
		passwordMatch = err == nil
	} else if h.config.Password != "" {
		// Fallback to plain text for migration period (should log warning)
		passwordMatch = subtle.ConstantTimeCompare([]byte(req.Password), []byte(h.config.Password)) == 1
	}

	if !usernameMatch || !passwordMatch {
		// Add slight delay to prevent timing attacks
		time.Sleep(100 * time.Millisecond)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate JWT token
	token, err := h.jwtManager.GenerateToken(req.Username)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Set token as cookie for browser-based clients
	SetTokenCookie(w, token, h.jwtManager.tokenExpiry)

	// Return token in response for API clients
	response := LoginResponse{
		Token:     token,
		ExpiresAt: time.Now().Add(h.jwtManager.tokenExpiry),
		Username:  req.Username,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// JWTLogoutHandler handles logout requests
type JWTLogoutHandler struct {
	jwtManager *JWTManager
}

// NewJWTLogoutHandler creates a new JWT logout handler
func NewJWTLogoutHandler(jm *JWTManager) *JWTLogoutHandler {
	return &JWTLogoutHandler{
		jwtManager: jm,
	}
}

// ServeHTTP handles the logout request
func (h *JWTLogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clear the token cookie
	ClearTokenCookie(w)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Logged out successfully",
	})
}

// JWTRefreshHandler handles token refresh requests
type JWTRefreshHandler struct {
	jwtManager *JWTManager
}

// NewJWTRefreshHandler creates a new JWT refresh handler
func NewJWTRefreshHandler(jm *JWTManager) *JWTRefreshHandler {
	return &JWTRefreshHandler{
		jwtManager: jm,
	}
}

// RefreshResponse represents the refresh token response
type RefreshResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ServeHTTP handles the token refresh request
func (h *JWTRefreshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract current token
	token := ExtractTokenFromRequest(r)
	if token == "" {
		http.Error(w, "No token provided", http.StatusUnauthorized)
		return
	}

	// Refresh the token
	newToken, err := h.jwtManager.RefreshToken(token)
	if err != nil {
		http.Error(w, "Failed to refresh token", http.StatusUnauthorized)
		return
	}

	// Set new token as cookie
	SetTokenCookie(w, newToken, h.jwtManager.tokenExpiry)

	// Return new token in response
	response := RefreshResponse{
		Token:     newToken,
		ExpiresAt: time.Now().Add(h.jwtManager.tokenExpiry),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// JWTAuthStatus handles authentication status checks
type JWTAuthStatus struct {
	jwtManager *JWTManager
}

// NewJWTAuthStatus creates a new auth status handler
func NewJWTAuthStatus(jm *JWTManager) *JWTAuthStatus {
	return &JWTAuthStatus{
		jwtManager: jm,
	}
}

// AuthStatusResponse represents the auth status response
type AuthStatusResponse struct {
	Authenticated bool      `json:"authenticated"`
	Username      string    `json:"username,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}

// ServeHTTP handles the auth status request
func (h *JWTAuthStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := ExtractTokenFromRequest(r)

	response := AuthStatusResponse{
		Authenticated: false,
	}

	if token != "" {
		claims, err := h.jwtManager.ValidateToken(token)
		if err == nil {
			response.Authenticated = true
			response.Username = claims.Username
			response.ExpiresAt = claims.ExpiresAt.Time
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
