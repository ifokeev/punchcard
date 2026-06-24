package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
}

func TestTokenMiddleware(t *testing.T) {
	h := tokenMiddleware("secret")(okHandler())
	// no header -> 401
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	// correct header -> 200
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestTokenMiddlewareDisabledWhenEmpty(t *testing.T) {
	h := tokenMiddleware("")(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("empty token should disable auth, got %d", rec.Code)
	}
}

func TestTokenMiddlewareAllowsHealthWithoutToken(t *testing.T) {
	h := tokenMiddleware("secret")(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Fatalf("health must bypass auth, got %d", rec.Code)
	}
}

func TestProxyMiddlewareStripsWhenUntrusted(t *testing.T) {
	var seen string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Forwarded-Proto")
	})
	h := proxyMiddleware(false)(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Fatalf("untrusted proxy header should be stripped, got %q", seen)
	}
}

func TestValidateBindFailSafe(t *testing.T) {
	if err := validateBind("127.0.0.1:8080", "", false); err != nil {
		t.Fatalf("loopback no-token should be allowed: %v", err)
	}
	if err := validateBind("0.0.0.0:8080", "", false); err == nil {
		t.Fatalf("non-loopback without token must fail closed")
	}
	if err := validateBind("0.0.0.0:8080", "", true); err != nil {
		t.Fatalf("--insecure should override: %v", err)
	}
	if err := validateBind("0.0.0.0:8080", "tok", false); err != nil {
		t.Fatalf("non-loopback WITH token is fine: %v", err)
	}
	// ':8080' has an empty host (bind-all-interfaces) and must NOT be treated as loopback
	if err := validateBind(":8080", "", false); err == nil {
		t.Fatalf("bind-all-interfaces ':8080' without token must fail closed")
	}
}
