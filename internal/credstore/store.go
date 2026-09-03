package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Credential struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

type Store struct {
	path  string
	key   []byte
	creds map[string]Credential
}

func NewStore(path string, key []byte) (*Store, error) {
	s := &Store{
		path:  path,
		key:   key,
		creds: make(map[string]Credential),
	}
	if _, err := os.Stat(path); err == nil {
		if err := s.load(); err != nil {
			return nil, fmt.Errorf("loading credential store: %w", err)
		}
	}
	return s, nil
}

func (s *Store) Get(key string) (Credential, error) {
	cred, ok := s.creds[key]
	if !ok {
		return Credential{}, fmt.Errorf("credential not found: %s", key)
	}
	return cred, nil
}

func (s *Store) Put(key string, cred Credential) error {
	s.creds[key] = cred
	return s.save()
}

func (s *Store) Delete(key string) error {
	delete(s.creds, key)
	return s.save()
}

func (s *Store) List() []string {
	keys := make([]string, 0, len(s.creds))
	for k := range s.creds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Store) save() error {
	plaintext, err := json.Marshal(s.creds)
	if err != nil {
		return err
	}
	ciphertext, err := encrypt(s.key, plaintext)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(s.path, ciphertext, 0600)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	plaintext, err := decrypt(s.key, data)
	if err != nil {
		return err
	}
	return json.Unmarshal(plaintext, &s.creds)
}

func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	data := ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, data, nil)
}
