package control

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var ErrUnauthorized = errors.New("unauthorized")

func (a *api) checkAuthMiddleware(r *http.Request) error {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return ErrUnauthorized
	}

	token := parts[1]
	for _, candidate := range a.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(candidate)) == 1 {
			return nil
		}
	}
	return ErrUnauthorized
}

func GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, bytes)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func LoadTokens(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open token file: %w", err)
	}
	defer file.Close()

	tokens := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if token := strings.TrimSpace(scanner.Text()); token != "" {
			tokens = append(tokens, token)
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("read token file: %w", scanner.Err())
	}

	return tokens, nil
}

func SaveTokens(path string, tokens []string) error {
	content := strings.Join(tokens, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set token file permissions: %w", err)
	}
	return nil
}
