package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type AuthMiddleware struct {
	secretKey string
}

type Claims struct {
	jwt.RegisteredClaims
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func NewAuthMiddleware(secretKey string) *AuthMiddleware {
	return &AuthMiddleware{
		secretKey: secretKey,
	}
}

// Middleware to check auth token
func (am *AuthMiddleware) Check(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health check
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from Authorization header or query param
		var token string

		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Verify token (simple bearer token for now)
		if token != am.secretKey {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Generate a JWT for WebSocket/streaming connections
func (am *AuthMiddleware) GenerateStreamToken() (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString([]byte(am.secretKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return ss, nil
}

// Verify JWT for streaming
func (am *AuthMiddleware) VerifyStreamToken(tokenString string) error {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(am.secretKey), nil
	})

	if err != nil {
		return fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("invalid token")
	}

	return nil
}
