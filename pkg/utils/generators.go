package utils

import (
	"github.com/sethvargo/go-password/password"
)

// GenerateSecurePassword generates secure a random password
func GenerateSecurePassword() (string, error) {
	return password.Generate(32, 10, 10, false, false)
}
