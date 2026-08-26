package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

func hashTemporaryPassword(password string) (string, error) {
	if len([]rune(password)) < 6 {
		return "", errors.New("临时密码至少需要6个字符")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordIterations, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived)), nil
}
