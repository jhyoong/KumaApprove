package credstore

import (
	"fmt"

	"golang.org/x/crypto/argon2"
)

var appSalt = []byte("kuma-approve-credential-store-v1")

func DeriveKey(machineID string) ([]byte, error) {
	if len(machineID) == 0 {
		return nil, fmt.Errorf("machine ID must not be empty")
	}
	// Argon2id: time=1, memory=64MB, threads=4, keyLen=32 (AES-256)
	key := argon2.IDKey([]byte(machineID), appSalt, 1, 64*1024, 4, 32)
	return key, nil
}
