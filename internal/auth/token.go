package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type TokenManager struct {
	secret []byte
}

type Claims struct {
	Subject string `json:"sub"`
	Expiry  int64  `json:"exp"`
}

func NewTokenManager(secret string) *TokenManager {
	sum := sha256.Sum256([]byte(secret))
	return &TokenManager{secret: sum[:]}
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func VerifyPassword(hash, password string) bool {
	expected := HashPassword(password)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(expected)) == 1
}

func (tm *TokenManager) Issue(subject string, ttl time.Duration) (string, error) {
	claims := Claims{
		Subject: subject,
		Expiry:  time.Now().Add(ttl).Unix(),
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signature := tm.sign(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + signature, nil
}

func (tm *TokenManager) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}

	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(tm.sign(payload))) != 1 {
		return nil, errors.New("invalid token signature")
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("invalid token claims")
	}

	if time.Now().Unix() > claims.Expiry {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

func (tm *TokenManager) sign(payload []byte) string {
	mac := hmac.New(sha256.New, tm.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
