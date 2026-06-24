package main

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
)

func tokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next // auth disabled
		}
		want := "Bearer " + token
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health checks are unauthenticated so liveness probes work without a token.
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			got := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func proxyMiddleware(trusted bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !trusted {
				r.Header.Del("X-Forwarded-Proto")
				r.Header.Del("X-Forwarded-Host")
				r.Header.Del("X-Forwarded-For")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validateBind(addr, token string, insecure bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSuffix(addr, "/")
	}
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if host == "" {
		loopback = false // empty host means bind-all-interfaces, not loopback
	} else if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if !loopback && token == "" && !insecure {
		return fmt.Errorf("refusing to bind non-loopback %q without --token (pass --insecure to override)", addr)
	}
	return nil
}
