package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHashAndCheckPassword(t *testing.T) {
	password := "s3cr3t-password"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	if !CheckPasswordHash(password, hash) {
		t.Fatal("expected password to match its hash")
	}

	if CheckPasswordHash("wrong-password", hash) {
		t.Fatal("expected wrong password to NOT match hash")
	}

	// malformed hash should not panic and should return false
	if CheckPasswordHash(password, "not-a-valid-hash") {
		t.Fatal("expected malformed hash to return false")
	}
}

func TestMakeAndValidateJWT(t *testing.T) {
	userID := uuid.New()
	secret := "test-secret"

	t.Run("valid token", func(t *testing.T) {
		token, err := MakeJWT(userID, secret, time.Minute)
		if err != nil {
			t.Fatalf("MakeJWT error: %v", err)
		}

		got, err := ValidateJWT(token, secret)
		if err != nil {
			t.Fatalf("ValidateJWT returned error for valid token: %v", err)
		}
		if got != userID {
			t.Fatalf("expected %v, got %v", userID, got)
		}
	})

	t.Run("invalid secret", func(t *testing.T) {
		token, err := MakeJWT(userID, secret, time.Minute)
		if err != nil {
			t.Fatalf("MakeJWT error: %v", err)
		}
		if _, err := ValidateJWT(token, "wrong-secret"); err == nil {
			t.Fatal("expected error when validating with wrong secret")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token, err := MakeJWT(userID, secret, -time.Minute)
		if err != nil {
			t.Fatalf("MakeJWT error: %v", err)
		}
		if _, err := ValidateJWT(token, secret); err == nil {
			t.Fatal("expected error when validating expired token")
		}
	})

	t.Run("malformed token", func(t *testing.T) {
		if _, err := ValidateJWT("not-a-token", secret); err == nil {
			t.Fatal("expected error when validating malformed token")
		}
	})
}
