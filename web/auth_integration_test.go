package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
	"golang.org/x/crypto/bcrypt"
)

func generateTestHash(password string) string {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(hash)
}

func TestServerWithAuthEnabled(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	authCfg := &webpkg.SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: generateTestHash("testpassword"),
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  5,
	}

	srv := webpkg.NewServerWithAuth("", sched, nil, nil, authCfg)
	if srv == nil {
		t.Fatal("NewServerWithAuth returned nil")
	}

	httpSrv := srv.HTTPServer()

	t.Run("api_requires_auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("auth_status_without_auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if resp["authEnabled"] != true {
			t.Error("expected authEnabled to be true")
		}
		if resp["authenticated"] != false {
			t.Error("expected authenticated to be false")
		}
	})

	t.Run("health_endpoints_bypass_auth", func(t *testing.T) {
		endpoints := []string{"/health", "/healthz", "/ready", "/live"}
		hc := webpkg.NewHealthChecker(nil, "test")
		srv.RegisterHealthEndpoints(hc)

		for _, ep := range endpoints {
			req := httptest.NewRequest("GET", ep, nil)
			w := httptest.NewRecorder()
			httpSrv.Handler.ServeHTTP(w, req)

			if w.Code == http.StatusUnauthorized {
				t.Errorf("%s should bypass auth, got status %d", ep, w.Code)
			}
		}
	})

	t.Run("static_files_bypass_auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code == http.StatusUnauthorized {
			t.Errorf("static files should bypass auth, got status %d", w.Code)
		}
	})
}

func TestLoginFlow(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	authCfg := &webpkg.SecureAuthConfig{
		Enabled:      true,
		Username:     "testuser",
		PasswordHash: generateTestHash("correctpassword"),
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  10,
	}

	srv := webpkg.NewServerWithAuth("", sched, nil, nil, authCfg)
	httpSrv := srv.HTTPServer()

	t.Run("login_with_valid_credentials", func(t *testing.T) {
		body := `{"username":"testuser","password":"correctpassword"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if resp["token"] == nil || resp["token"] == "" {
			t.Error("expected token in response")
		}

		cookies := w.Result().Cookies()
		var authCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "auth_token" {
				authCookie = c
				break
			}
		}
		if authCookie == nil {
			t.Error("expected auth_token cookie")
		}
	})

	t.Run("login_with_invalid_password", func(t *testing.T) {
		body := `{"username":"testuser","password":"wrongpassword"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("login_with_invalid_username", func(t *testing.T) {
		body := `{"username":"wronguser","password":"correctpassword"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("login_method_not_allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/login", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})
}

func TestAuthenticatedAccess(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	job := &testJob{}
	job.Name = "auth-test-job"
	job.Schedule = "@daily"
	job.Command = "echo"
	_ = sched.AddJob(job)

	authCfg := &webpkg.SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: generateTestHash("password123"),
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  10,
	}

	srv := webpkg.NewServerWithAuth("", sched, nil, nil, authCfg)
	httpSrv := srv.HTTPServer()

	body := `{"username":"admin","password":"password123"}`
	loginReq := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginW := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(loginW, loginReq)

	if loginW.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginW.Code)
	}

	var loginResp map[string]interface{}
	_ = json.NewDecoder(loginW.Body).Decode(&loginResp)
	token := loginResp["token"].(string)

	t.Run("access_with_bearer_token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("access_with_cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("access_with_invalid_token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("auth_status_when_authenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(w.Body).Decode(&resp)

		if resp["authenticated"] != true {
			t.Error("expected authenticated to be true")
		}
		if resp["username"] != "admin" {
			t.Errorf("expected username 'admin', got %v", resp["username"])
		}
	})
}

func TestLogoutFlow(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	authCfg := &webpkg.SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: generateTestHash("password"),
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  10,
	}

	srv := webpkg.NewServerWithAuth("", sched, nil, nil, authCfg)
	httpSrv := srv.HTTPServer()

	body := `{"username":"admin","password":"password"}`
	loginReq := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginW := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(loginW, loginReq)

	var loginResp map[string]interface{}
	_ = json.NewDecoder(loginW.Body).Decode(&loginResp)
	token := loginResp["token"].(string)

	t.Run("logout_revokes_token", func(t *testing.T) {
		logoutReq := httptest.NewRequest("POST", "/api/logout", nil)
		logoutReq.Header.Set("Authorization", "Bearer "+token)
		logoutW := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(logoutW, logoutReq)

		if logoutW.Code != http.StatusOK {
			t.Errorf("logout expected status 200, got %d", logoutW.Code)
		}

		cookies := logoutW.Result().Cookies()
		var authCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "auth_token" {
				authCookie = c
				break
			}
		}
		if authCookie == nil || authCookie.MaxAge != -1 {
			t.Error("expected auth_token cookie to be cleared")
		}

		jobsReq := httptest.NewRequest("GET", "/api/jobs", nil)
		jobsReq.Header.Set("Authorization", "Bearer "+token)
		jobsW := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(jobsW, jobsReq)

		if jobsW.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 after logout, got %d", jobsW.Code)
		}
	})

	t.Run("logout_method_not_allowed", func(t *testing.T) {
		body := `{"username":"admin","password":"password"}`
		newLoginReq := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		newLoginReq.Header.Set("Content-Type", "application/json")
		newLoginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
		newLoginW := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(newLoginW, newLoginReq)

		var newLoginResp map[string]interface{}
		_ = json.NewDecoder(newLoginW.Body).Decode(&newLoginResp)
		newToken := newLoginResp["token"].(string)

		req := httptest.NewRequest("GET", "/api/logout", nil)
		req.Header.Set("Authorization", "Bearer "+newToken)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})
}

func TestServerWithoutAuth(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	job := &testJob{}
	job.Name = "no-auth-job"
	job.Schedule = "@daily"
	job.Command = "echo"
	_ = sched.AddJob(job)

	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("api_accessible_without_auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("auth_status_shows_disabled", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			var resp map[string]interface{}
			_ = json.NewDecoder(w.Body).Decode(&resp)

			if resp["authEnabled"] != false {
				t.Error("expected authEnabled to be false when auth disabled")
			}
		}
	})
}

func TestCSRFTokenEndpoint(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	authCfg := &webpkg.SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: generateTestHash("password"),
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  10,
	}

	srv := webpkg.NewServerWithAuth("", sched, nil, nil, authCfg)
	httpSrv := srv.HTTPServer()

	t.Run("get_csrf_token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/csrf-token", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]string
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if resp["csrf_token"] == "" {
			t.Error("expected non-empty csrf_token")
		}
	})
}
