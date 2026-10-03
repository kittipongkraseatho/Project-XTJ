package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_DeterministicFormat(t *testing.T) {
	hash, err := HashPassword("Sup3rSecret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=") {
		t.Errorf("unexpected hash prefix: %q", hash)
	}
	if !strings.Contains(hash, ",t=1,p=2$") {
		t.Errorf("hash missing argon2id params: %q", hash)
	}
}

func TestHashPassword_DifferentSalts(t *testing.T) {
	a, _ := HashPassword("Sup3rSecret")
	b, _ := HashPassword("Sup3rSecret")
	if a == b {
		t.Error("two hashes of same password should differ (salt)")
	}
}

func TestVerifyPassword_Success(t *testing.T) {
	hash, _ := HashPassword("Sup3rSecret")
	if err := VerifyPassword("Sup3rSecret", hash); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	hash, _ := HashPassword("Sup3rSecret")
	err := VerifyPassword("wrong", hash)
	if err != ErrInvalidPassword {
		t.Errorf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	if err := VerifyPassword("x", "not-an-argon2id-hash"); err == nil {
		t.Error("expected error for malformed hash")
	}
}

func TestIsStrongEnough(t *testing.T) {
	tests := []struct {
		in       string
		wantErr  bool
	}{
		{"Sup3rSecret", false},   // 11 chars + letter + digit → ok
		{"short", true},           // too short
		{"longnodigit", true},     // letters but no digit
		{"12345678", true},        // digits only, no letters
	}
	for _, tc := range tests {
		got := IsStrongEnough(tc.in)
		if (got != nil) != tc.wantErr {
			t.Errorf("IsStrongEnough(%q) err=%v wantErr=%v", tc.in, got, tc.wantErr)
		}
	}
}

func TestNewRefreshToken(t *testing.T) {
	tok, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if tok == "" || hash == "" {
		t.Error("token and hash must be non-empty")
	}
	if tok == hash {
		t.Error("token and hash should differ")
	}
	if h2 := HashRefreshToken(tok); h2 != hash {
		t.Error("HashRefreshToken(tok) should return the same hash")
	}
}