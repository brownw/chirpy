package auth

import (
	"github.com/alexedwards/argon2id"
)

func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return hash, nil
}

func CheckPasswordHash(password, hash string) bool {
	isMatch, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false
	}
	return isMatch
}
