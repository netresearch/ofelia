package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
)

var (
	// ErrInvalidToken is returned when a token is invalid
	ErrInvalidToken = errors.New("invalid token")
)

// AuthProvider interface for authentication implementations
type AuthProvider interface {
	GenerateToken(username string) (string, error)
	ValidateToken(token string) (interface{}, error)
	Middleware(next http.Handler) http.Handler
}

// LegacyAuthProvider wraps the old TokenManager for compatibility
type LegacyAuthProvider struct {
	tm *TokenManager
}

// NewLegacyAuthProvider creates a legacy auth provider
func NewLegacyAuthProvider(secretKey string, expiryHours int) *LegacyAuthProvider {
	return &LegacyAuthProvider{
		tm: NewTokenManager(secretKey, expiryHours),
	}
}

func (l *LegacyAuthProvider) GenerateToken(username string) (string, error) {
	return l.tm.GenerateToken(username)
}

func (l *LegacyAuthProvider) ValidateToken(token string) (interface{}, error) {
	data, valid := l.tm.ValidateToken(token)
	if !valid {
		return nil, ErrInvalidToken
	}
	return data, nil
}

func (l *LegacyAuthProvider) Middleware(next http.Handler) http.Handler {
	return authMiddleware(l.tm, true)(next)
}

// JWTAuthProvider wraps the new JWTManager for compatibility
type JWTAuthProvider struct {
	jm *JWTManager
}

// NewJWTAuthProvider creates a JWT auth provider
func NewJWTAuthProvider(secretKey string, expiryHours int) (*JWTAuthProvider, error) {
	jm, err := NewJWTManager(secretKey, expiryHours)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT manager: %w", err)
	}
	return &JWTAuthProvider{
		jm: jm,
	}, nil
}

func (j *JWTAuthProvider) GenerateToken(username string) (string, error) {
	return j.jm.GenerateToken(username)
}

func (j *JWTAuthProvider) ValidateToken(token string) (interface{}, error) {
	return j.jm.ValidateToken(token)
}

func (j *JWTAuthProvider) Middleware(next http.Handler) http.Handler {
	return j.jm.Middleware(next)
}

// CreateAuthProvider creates the appropriate auth provider based on configuration
func CreateAuthProvider(config *AuthConfig) (AuthProvider, error) {
	// Check environment variable for auth type
	authType := os.Getenv("OFELIA_AUTH_TYPE")
	if authType == "" {
		authType = "jwt" // Default to JWT for new installations
	}
	
	switch authType {
	case "legacy":
		return NewLegacyAuthProvider(config.SecretKey, config.TokenExpiry), nil
	case "jwt":
		fallthrough
	default:
		return NewJWTAuthProvider(config.SecretKey, config.TokenExpiry)
	}
}

// MigrateAuthToken attempts to migrate a legacy token to JWT
func MigrateAuthToken(legacyProvider *LegacyAuthProvider, jwtProvider AuthProvider, legacyToken string) (string, error) {
	// Validate the legacy token
	data, err := legacyProvider.ValidateToken(legacyToken)
	if err != nil {
		return "", err
	}
	
	// Extract username from legacy token data
	tokenData, ok := data.(*TokenData)
	if !ok {
		return "", ErrInvalidToken
	}
	
	// Generate new JWT token
	return jwtProvider.GenerateToken(tokenData.Username)
}

// AuthMigrationMiddleware handles gradual migration from legacy to JWT
func AuthMigrationMiddleware(legacy *LegacyAuthProvider, jwt *JWTAuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ExtractTokenFromRequest(r)
			if token == "" {
				http.Error(w, "Missing authorization", http.StatusUnauthorized)
				return
			}
			
			// Try JWT first
			if _, err := jwt.ValidateToken(token); err == nil {
				next.ServeHTTP(w, r)
				return
			}
			
			// Try legacy token and migrate if valid
			if _, err := legacy.ValidateToken(token); err == nil {
				// Generate new JWT token
				if newToken, err := MigrateAuthToken(legacy, jwt, token); err == nil {
					// Set new token as cookie
					SetTokenCookie(w, newToken, jwt.jm.tokenExpiry)
					// Add new token to response header for client migration
					w.Header().Set("X-New-Token", newToken)
				}
				next.ServeHTTP(w, r)
				return
			}
			
			http.Error(w, "Invalid token", http.StatusUnauthorized)
		})
	}
}