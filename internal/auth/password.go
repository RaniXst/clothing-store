package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 19 * 1024
	argonIterations  = 2
	argonParallelism = 1
	argonKeyLength   = 32
	saltLength       = 16
)

type PasswordHasher struct{}

func (PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func (PasswordHasher) Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false
	}
	memory, okM := parseParam(params[0], "m=")
	iterations, okT := parseParam(params[1], "t=")
	parallelism, okP := parseParam(params[2], "p=")
	if !okM || !okT || !okP || memory < 8*1024 || memory > 64*1024 || iterations < 1 || iterations > 4 || parallelism < 1 || parallelism > 2 {
		return false
	}
	salt, errSalt := base64.RawStdEncoding.DecodeString(parts[4])
	expected, errHash := base64.RawStdEncoding.DecodeString(parts[5])
	if errSalt != nil || errHash != nil || len(salt) < 8 || len(salt) > 64 || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parseParam(value, prefix string) (int, bool) {
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	parsed, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	return parsed, err == nil
}
