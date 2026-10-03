package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

// writeTestKey generates an Ed25519 PKCS8 PEM to a temp file and returns its path.
func writeTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	tmp := filepath.Join(t.TempDir(), "ed25519.pem")
	if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return tmp
}

func TestNewJWTService_LoadExistingKey(t *testing.T) {
	keyPath := writeTestKey(t)
	svc, err := NewJWTService(keyPath, "test", "test-web", 15*time.Minute)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if svc == nil {
		t.Fatal("svc is nil")
	}
}

func TestNewJWTService_GenerateKeyIfMissing(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ed25519.pem")
	svc, err := NewJWTService(keyPath, "test", "test-web", 15*time.Minute)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if svc == nil {
		t.Fatal("svc is nil")
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("expected key file at %s: %v", keyPath, err)
	}
}

func TestSignAndVerify_RoundTrip(t *testing.T) {
	keyPath := writeTestKey(t)
	svc, _ := NewJWTService(keyPath, "iss", "aud", 5*time.Minute)
	uid, _ := uuid.NewV7()
	tid, _ := uuid.NewV7()
	sid, _ := uuid.NewV7()
	tok, exp, err := svc.Sign(SignInput{UserID: uid, TenantID: tid, SessionID: sid, Role: "owner"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if tok == "" || exp.IsZero() {
		t.Error("token + exp should be populated")
	}
	claims, err := svc.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != uid.String() {
		t.Errorf("sub=%s want %s", claims.Subject, uid.String())
	}
	if claims.TenantID != tid.String() {
		t.Errorf("tid=%s want %s", claims.TenantID, tid.String())
	}
	if claims.Role != "owner" {
		t.Errorf("role=%s want owner", claims.Role)
	}
}

func TestVerify_RejectsTamperedToken(t *testing.T) {
	keyPath := writeTestKey(t)
	svc, _ := NewJWTService(keyPath, "iss", "aud", 5*time.Minute)
	uid, _ := uuid.NewV7()
	tid, _ := uuid.NewV7()
	sid, _ := uuid.NewV7()
	tok, _, _ := svc.Sign(SignInput{UserID: uid, TenantID: tid, SessionID: sid, Role: "owner"})

	// Tamper by appending a char
	if _, err := svc.Verify(tok + "x"); err == nil {
		t.Error("expected error for tampered token")
	}
}