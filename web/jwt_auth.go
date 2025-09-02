package web

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTManager handles JWT-based authentication
type JWTManager struct {
	secretKey   []byte
	tokenExpiry time.Duration
}

// Claims represents the JWT claims
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(secretKey string, expiryHours int) (*JWTManager, error) {
	if secretKey == "" {
		// Generate a random key if not provided
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("failed to generate secure random key: %w", err)
		}
		secretKey = base64.StdEncoding.EncodeToString(key)
		// This is a security risk as the key will be different on each restart
		fmt.Println("WARNING: Using auto-generated JWT secret key - provide explicit secret for production")
	}
	
	// Validate key length for security
	if len(secretKey) < 32 {
		return nil, fmt.Errorf("JWT secret key must be at least 32 characters long")
	}
	
	return &JWTManager{
		secretKey:   []byte(secretKey),
		tokenExpiry: time.Duration(expiryHours) * time.Hour,
	}, nil
}

// GenerateToken creates a new JWT token
func (jm *JWTManager) GenerateToken(username string) (string, error) {
	// Create the claims
	claims := &Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jm.tokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "ofelia",
			Subject:   username,
		},
	}
	
	// Create the token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	
	// Sign the token with the secret key
	tokenString, err := token.SignedString(jm.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	
	return tokenString, nil
}

// ValidateToken validates a JWT token and returns the claims
func (jm *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jm.secretKey, nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	
	// Extract claims
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	
	return claims, nil
}

// RefreshToken generates a new token with extended expiry
func (jm *JWTManager) RefreshToken(tokenString string) (string, error) {
	// Validate the existing token
	claims, err := jm.ValidateToken(tokenString)
	if err != nil {
		return "", fmt.Errorf("cannot refresh invalid token: %w", err)
	}
	
	// Generate a new token with the same username
	return jm.GenerateToken(claims.Username)
}

// Middleware creates an HTTP middleware for JWT authentication
func (jm *JWTManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for login endpoint
		if r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}
		
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing authorization header", http.StatusUnauthorized)
			return
		}
		
		// Check for Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}
		
		tokenString := parts[1]
		
		// Validate the token
		claims, err := jm.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		
		// Store username in request context for later use
		r.Header.Set("X-Username", claims.Username)
		
		// Continue to the next handler
		next.ServeHTTP(w, r)
	})
}

// ExtractTokenFromRequest extracts the JWT token from an HTTP request
func ExtractTokenFromRequest(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}
	
	// Try cookie as fallback
	cookie, err := r.Cookie("token")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}
	
	// No token found
	return ""
}

// SetTokenCookie sets a JWT token as an HTTP-only cookie
func SetTokenCookie(w http.ResponseWriter, token string, expiry time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true, // Enable in production with HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(expiry.Seconds()),
	})
}

// ClearTokenCookie removes the JWT token cookie
func ClearTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1, // Delete the cookie
	})
}