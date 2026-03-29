package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndAuthenticate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s, err := NewUserStore(f)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.AddUser("alice", "pass123"); err != nil {
		t.Fatal(err)
	}

	if !s.Authenticate("alice", "pass123") {
		t.Error("expected auth success")
	}
	if s.Authenticate("alice", "wrong") {
		t.Error("expected auth failure with wrong password")
	}
	if s.Authenticate("bob", "pass123") {
		t.Error("expected auth failure with unknown user")
	}
}

func TestChangePassword(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s, _ := NewUserStore(f)
	s.AddUser("alice", "old")

	if err := s.ChangePassword("alice", "new"); err != nil {
		t.Fatal(err)
	}

	if s.Authenticate("alice", "old") {
		t.Error("old password should not work")
	}
	if !s.Authenticate("alice", "new") {
		t.Error("new password should work")
	}
}

func TestChangePassword_NonExistent(t *testing.T) {
	s, _ := NewUserStore("")
	if err := s.ChangePassword("ghost", "pass"); err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestRemoveUser(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s, _ := NewUserStore(f)
	s.AddUser("alice", "pass")
	s.RemoveUser("alice")

	if s.Authenticate("alice", "pass") {
		t.Error("removed user should not authenticate")
	}
}

func TestPersistence(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s1, _ := NewUserStore(f)
	s1.AddUser("alice", "pass1")
	s1.AddUser("bob", "pass2")

	// Reload from file
	s2, err := NewUserStore(f)
	if err != nil {
		t.Fatal(err)
	}

	if !s2.Authenticate("alice", "pass1") {
		t.Error("alice should persist")
	}
	if !s2.Authenticate("bob", "pass2") {
		t.Error("bob should persist")
	}
}

func TestListUsers(t *testing.T) {
	s, _ := NewUserStore("")
	s.AddUser("alice", "a")
	s.AddUser("bob", "b")

	users := s.ListUsers()
	if len(users) != 2 {
		t.Errorf("got %d users, want 2", len(users))
	}
}

func TestHasUsers(t *testing.T) {
	s, _ := NewUserStore("")
	if s.HasUsers() {
		t.Error("should have no users")
	}
	s.AddUser("alice", "a")
	if !s.HasUsers() {
		t.Error("should have users")
	}
}

func TestFilePermissions(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s, _ := NewUserStore(f)
	s.AddUser("alice", "pass")

	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestPasswordHash_DifferentSalts(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.json")
	s, _ := NewUserStore(f)

	// Same password, different users should have different hashes
	s.AddUser("alice", "samepass")
	s.AddUser("bob", "samepass")

	s.mu.RLock()
	alice := s.users["alice"]
	bob := s.users["bob"]
	s.mu.RUnlock()

	if alice.Salt == bob.Salt {
		t.Error("salts should differ")
	}
	if alice.PassHash == bob.PassHash {
		t.Error("hashes should differ due to different salts")
	}
}
