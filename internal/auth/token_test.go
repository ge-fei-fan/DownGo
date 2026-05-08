package auth

import (
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	manager := NewTokenManager("secret")

	token, err := manager.Issue("tester", time.Minute)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Subject != "tester" {
		t.Fatalf("unexpected subject %q", claims.Subject)
	}
}

func TestVerifyPassword(t *testing.T) {
	hash := HashPassword("top-secret")
	if !VerifyPassword(hash, "top-secret") {
		t.Fatal("expected password verification to succeed")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("expected password verification to fail")
	}
}
