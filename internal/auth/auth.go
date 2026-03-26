package auth

import (
	"log"

	"github.com/alexedwards/argon2id"
)

func HashPassword(password string) (string, error) {
	hashPass, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		log.Fatal("Error hashing password")
	}
	return hashPass, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		log.Fatal("Error comparing password to hash")
	}
	return match, nil
}
