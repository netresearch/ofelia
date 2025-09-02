package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJWTManager_GenerateAndValidateToken(t *testing.T) {
	jm, _ := NewJWTManager("test-secret-key-that-is-long-enough-for-jwt", 1)
	
	// Generate a token
	token, err := jm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}
	
	// Validate the token
	claims, err := jm.ValidateToken(token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}
	
	// Check claims
	if claims.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", claims.Username)
	}
	
	// Check expiry
	if claims.ExpiresAt.Time.Before(time.Now()) {
		t.Error("Token is already expired")
	}
	
	if claims.ExpiresAt.Time.After(time.Now().Add(2 * time.Hour)) {
		t.Error("Token expiry is too far in the future")
	}
}

func TestJWTManager_InvalidToken(t *testing.T) {
	jm, _ := NewJWTManager("test-secret-key-that-is-long-enough-for-jwt", 1)
	
	// Test with invalid token
	_, err := jm.ValidateToken("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid token, got nil")
	}
	
	// Test with empty token
	_, err = jm.ValidateToken("")
	if err == nil {
		t.Error("Expected error for empty token, got nil")
	}
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	// Create manager with very short expiry
	jm, _ := NewJWTManager("test-secret-key-that-is-long-enough-for-jwt", 0) // 0 hours = already expired
	
	token, err := jm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}
	
	// Token should be invalid due to expiry
	_, err = jm.ValidateToken(token)
	if err == nil {
		t.Error("Expected error for expired token, got nil")
	}
}

func TestJWTManager_RefreshToken(t *testing.T) {
	jm, _ := NewJWTManager("test-secret-key-that-is-long-enough-for-jwt", 1)
	
	// Generate initial token
	token1, err := jm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}
	
	// Refresh the token
	token2, err := jm.RefreshToken(token1)
	if err != nil {
		t.Fatalf("Failed to refresh token: %v", err)
	}
	
	// Both tokens should be valid
	claims1, err := jm.ValidateToken(token1)
	if err != nil {
		t.Errorf("Original token should still be valid: %v", err)
	}
	
	claims2, err := jm.ValidateToken(token2)
	if err != nil {
		t.Errorf("Refreshed token should be valid: %v", err)
	}
	
	// Username should be the same
	if claims1.Username != claims2.Username {
		t.Errorf("Username mismatch: %s != %s", claims1.Username, claims2.Username)
	}
}

func TestJWTManager_Middleware(t *testing.T) {
	jm, _ := NewJWTManager("test-secret-key-that-is-long-enough-for-jwt", 1)
	
	// Generate a valid token
	token, _ := jm.GenerateToken("testuser")
	
	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := r.Header.Get("X-Username")
		w.Write([]byte("OK:" + username))
	})
	
	// Wrap with middleware
	protected := jm.Middleware(testHandler)
	
	tests := []struct {
		name           string
		authorization  string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid token",
			authorization:  "Bearer " + token,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK:testuser",
		},
		{
			name:           "Missing authorization",
			authorization:  "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Missing authorization header",
		},
		{
			name:           "Invalid format",
			authorization:  token, // Missing "Bearer " prefix
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid authorization header format",
		},
		{
			name:           "Invalid token",
			authorization:  "Bearer invalid-token",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid or expired token",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			
			body := strings.TrimSpace(rec.Body.String())
			if !strings.Contains(body, tt.expectedBody) {
				t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedBody, body)
			}
		})
	}
}

func TestJWTLoginHandler(t *testing.T) {
	config := &AuthConfig{
		Enabled:     true,
		Username:    "admin",
		Password:    "secret",
		SecretKey:   "test-secret-key-that-is-long-enough-for-jwt",
		TokenExpiry: 1,
	}
	
	jm, _ := NewJWTManager(config.SecretKey, config.TokenExpiry)
	handler := NewJWTLoginHandler(config, jm)
	
	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
	}{
		{
			name:           "Valid credentials",
			method:         "POST",
			body:           `{"username":"admin","password":"secret"}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid credentials",
			method:         "POST",
			body:           `{"username":"admin","password":"wrong"}`,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Invalid method",
			method:         "GET",
			body:           "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "Invalid JSON",
			method:         "POST",
			body:           `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			
			// Check response for successful login
			if tt.expectedStatus == http.StatusOK {
				var response LoginResponse
				if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				
				if response.Token == "" {
					t.Error("Expected token in response")
				}
				
				if response.Username != "admin" {
					t.Errorf("Expected username 'admin', got '%s'", response.Username)
				}
				
				// Check for cookie
				cookies := rec.Result().Cookies()
				found := false
				for _, cookie := range cookies {
					if cookie.Name == "token" {
						found = true
						if cookie.HttpOnly != true {
							t.Error("Token cookie should be HttpOnly")
						}
						break
					}
				}
				if !found {
					t.Error("Token cookie not set")
				}
			}
		})
	}
}

func TestExtractTokenFromRequest(t *testing.T) {
	tests := []struct {
		name          string
		setupRequest  func(*http.Request)
		expectedToken string
	}{
		{
			name: "Bearer token in Authorization header",
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer test-token")
			},
			expectedToken: "test-token",
		},
		{
			name: "Token in cookie",
			setupRequest: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})
			},
			expectedToken: "cookie-token",
		},
		{
			name: "Token in query parameter (should be ignored for security)",
			setupRequest: func(r *http.Request) {
				q := r.URL.Query()
				q.Add("token", "query-token")
				r.URL.RawQuery = q.Encode()
			},
			expectedToken: "", // Query parameter tokens are ignored for security
		},
		{
			name: "No token",
			setupRequest: func(r *http.Request) {
				// No token setup
			},
			expectedToken: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test?", nil)
			tt.setupRequest(req)
			
			token := ExtractTokenFromRequest(req)
			if token != tt.expectedToken {
				t.Errorf("Expected token '%s', got '%s'", tt.expectedToken, token)
			}
		})
	}
}

func TestAuthMigration(t *testing.T) {
	legacy := NewLegacyAuthProvider("test-secret-key-that-is-long-enough-for-jwt", 1)
	jwt, _ := NewJWTAuthProvider("test-secret-key-that-is-long-enough-for-jwt", 1)
	
	// Generate a legacy token
	legacyToken, err := legacy.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate legacy token: %v", err)
	}
	
	// Migrate the token
	jwtToken, err := MigrateAuthToken(legacy, jwt, legacyToken)
	if err != nil {
		t.Fatalf("Failed to migrate token: %v", err)
	}
	
	// Validate the new JWT token
	claims, err := jwt.jm.ValidateToken(jwtToken)
	if err != nil {
		t.Fatalf("Failed to validate migrated token: %v", err)
	}
	
	if claims.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", claims.Username)
	}
}