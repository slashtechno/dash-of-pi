package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

type AuthMiddleware struct {
	secretKey string
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func NewAuthMiddleware(secretKey string) *AuthMiddleware {
	return &AuthMiddleware{secretKey: secretKey}
}

// Check validates the bearer token from the Authorization header or ?token= query param.
func (am *AuthMiddleware) Check(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		var token string

		if authHeader := r.Header.Get("Authorization"); authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token == "" || token != am.secretKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
