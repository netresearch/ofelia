package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenManager(t *testing.T) {
	tm := NewTokenManager("test-secret-key", 1)

	// Test token generation
	token, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token == "" {
		t.Error("Generated token is empty")
	}

	// Test token validation
	data, valid := tm.ValidateToken(token)
	if !valid {
		t.Error("Valid token was rejected")
	}

	if data.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", data.Username)
	}

	// Test invalid token
	_, valid = tm.ValidateToken("invalid-token")
	if valid {
		t.Error("Invalid token was accepted")
	}

	// Test token revocation
	tm.RevokeToken(token)
	_, valid = tm.ValidateToken(token)
	if valid {
		t.Error("Revoked token was still valid")
	}

	t.Log("Token manager tests passed")
}

func TestTokenExpiry(t *testing.T) {
	// Create token manager with very short expiry
	tm := &TokenManager{
		secretKey:   []byte("test"),
		tokens:      make(map[string]*TokenData),
		tokenExpiry: 100 * time.Millisecond,
	}

	token, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Token should be valid initially
	_, valid := tm.ValidateToken(token)
	if !valid {
		t.Error("Fresh token was invalid")
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Token should be expired
	_, valid = tm.ValidateToken(token)
	if valid {
		t.Error("Expired token was still valid")
	}

	t.Log("Token expiry test passed")
}

func TestLoginHandler(t *testing.T) {
	config := &AuthConfig{
		Enabled:     true,
		Username:    "admin",
		Password:    "secret123",
		SecretKey:   "test-key",
		TokenExpiry: 1,
	}

	tm := NewTokenManager(config.SecretKey, config.TokenExpiry)
	handler := NewLoginHandler(config, tm)

	// Test successful login
	credentials := map[string]string{
		"username": "admin",
		"password": "secret123",
	}
	body, _ := json.Marshal(credentials)

	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check response
	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["token"] == nil || response["token"] == "" {
		t.Error("No token in response")
	}

	// Check cookie
	cookies := w.Result().Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "auth_token" {
			found = true
			if cookie.HttpOnly != true {
				t.Error("Cookie should be HttpOnly")
			}
			if cookie.SameSite != http.SameSiteStrictMode {
				t.Error("Cookie should have SameSite=Strict")
			}
			break
		}
	}
	if !found {
		t.Error("Auth cookie not set")
	}

	// Test invalid credentials
	credentials["password"] = "wrong"
	body, _ = json.Marshal(credentials)

	req = httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for invalid credentials, got %d", w.Code)
	}

	t.Log("Login handler tests passed")
}

func TestAuthMiddleware(t *testing.T) {
	tm := NewTokenManager("test-secret", 1)
	token, _ := tm.GenerateToken("testuser")

	// Create a protected handler
	protectedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-Auth-User")
		w.Write([]byte("Protected: " + user))
	})

	// Wrap with auth middleware
	middleware := authMiddleware(tm, true)
	handler := middleware(protectedHandler)

	// Test without token
	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 without token, got %d", w.Code)
	}

	// Test with valid token in header
	req = httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with valid token, got %d", w.Code)
	}

	if w.Body.String() != "Protected: testuser" {
		t.Errorf("Expected 'Protected: testuser', got '%s'", w.Body.String())
	}

	// Test with token in cookie
	req = httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{
		Name:  "auth_token",
		Value: token,
	})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with token in cookie, got %d", w.Code)
	}

	// Test with invalid token
	req = httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with invalid token, got %d", w.Code)
	}

	t.Log("Auth middleware tests passed")
}

func TestLogoutHandler(t *testing.T) {
	tm := NewTokenManager("test-secret", 1)
	token, _ := tm.GenerateToken("testuser")

	handler := NewLogoutHandler(tm)

	// Test logout with token in header
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Token should be revoked
	_, valid := tm.ValidateToken(token)
	if valid {
		t.Error("Token was not revoked after logout")
	}

	// Check cookie is cleared
	cookies := w.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "auth_token" {
			if cookie.MaxAge != -1 {
				t.Error("Auth cookie was not cleared properly")
			}
			break
		}
	}

	t.Log("Logout handler tests passed")
}
