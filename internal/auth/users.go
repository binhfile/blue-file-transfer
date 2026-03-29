package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// User represents a user account.
type User struct {
	Username string `json:"username"`
	PassHash string `json:"pass_hash"` // hex(SHA256(salt + password))
	Salt     string `json:"salt"`      // hex-encoded random salt
}

// UserStore manages user accounts with file persistence.
type UserStore struct {
	mu       sync.RWMutex
	users    map[string]*User
	filePath string
}

// NewUserStore creates or loads a user store from file.
func NewUserStore(filePath string) (*UserStore, error) {
	s := &UserStore{
		users:    make(map[string]*User),
		filePath: filePath,
	}
	if filePath != "" {
		if err := s.load(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("load users: %w", err)
		}
	}
	return s, nil
}

// AddUser adds or updates a user with the given password.
func (s *UserStore) AddUser(username, password string) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	saltHex := hex.EncodeToString(salt)
	hash := hashPassword(password, saltHex)

	s.mu.Lock()
	s.users[username] = &User{
		Username: username,
		PassHash: hash,
		Salt:     saltHex,
	}
	s.mu.Unlock()

	return s.saveLocked()
}

// RemoveUser removes a user.
func (s *UserStore) RemoveUser(username string) error {
	s.mu.Lock()
	delete(s.users, username)
	s.mu.Unlock()
	return s.saveLocked()
}

// Authenticate checks username and password. Returns true if valid.
func (s *UserStore) Authenticate(username, password string) bool {
	s.mu.RLock()
	user, ok := s.users[username]
	s.mu.RUnlock()

	if !ok {
		return false
	}

	hash := hashPassword(password, user.Salt)
	return hash == user.PassHash
}

// ChangePassword changes a user's password. Returns error if user doesn't exist.
func (s *UserStore) ChangePassword(username, newPassword string) error {
	s.mu.Lock()
	user, ok := s.users[username]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("user not found: %s", username)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("generate salt: %w", err)
	}

	user.Salt = hex.EncodeToString(salt)
	user.PassHash = hashPassword(newPassword, user.Salt)
	s.mu.Unlock()

	return s.saveLocked()
}

// ListUsers returns all usernames.
func (s *UserStore) ListUsers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.users))
	for name := range s.users {
		names = append(names, name)
	}
	return names
}

// HasUsers returns true if any users are configured.
func (s *UserStore) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

func hashPassword(password, saltHex string) string {
	h := sha256.New()
	h.Write([]byte(saltHex))
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *UserStore) load() error {
	if s.filePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var users []*User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse users file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range users {
		s.users[u.Username] = u
	}
	return nil
}

func (s *UserStore) saveLocked() error {
	if s.filePath == "" {
		return nil
	}

	s.mu.RLock()
	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}

	return os.WriteFile(s.filePath, data, 0600)
}
