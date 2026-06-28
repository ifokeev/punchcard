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
		wantBearer := "Bearer " + token
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CLI / loop send the token as a bearer header.
			ok := subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(wantBearer)) == 1
			// Browsers can't set a bearer header on a navigation, so also accept HTTP
			// Basic where the password is the token (username ignored). The browser
			// caches it and replays it on the page AND every fetch().
			if !ok {
				if _, pass, has := r.BasicAuth(); has &&
					subtle.ConstantTimeCompare([]byte(pass), []byte(token)) == 1 {
					ok = true
				}
			}
			if !ok {
				// Prompt a browser navigation for credentials; stay silent for API/XHR
				// (Accept: application/json) so fetch() 401s don't pop a second dialog.
				if strings.Contains(r.Header.Get("Accept"), "text/html") {
					w.Header().Set("WWW-Authenticate", `Basic realm="punchcard"`)
				}
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
