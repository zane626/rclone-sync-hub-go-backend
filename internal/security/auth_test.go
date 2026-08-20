package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthServiceIssuesAndVerifiesRoleToken(t *testing.T) {
	service, err := NewAuthService(AuthConfig{
		Enabled:       true,
		AdminUsername: "admin",
		AdminPassword: "correct-horse-battery-staple",
		TokenSecret:   strings.Repeat("s", 32),
		TokenTTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	token, issued, err := service.Login("admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Subject != issued.Subject || verified.Role != RoleAdmin {
		t.Fatalf("verified claims=%+v issued=%+v", verified, issued)
	}

	tampered := token[:len(token)-1] + "x"
	if _, err := service.Verify(tampered); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	if _, _, err := service.Login("admin", "wrong-password"); err == nil {
		t.Fatal("wrong password must be rejected")
	}
}

func TestAuthServiceNormalizesConfiguredUsername(t *testing.T) {
	service, err := NewAuthService(AuthConfig{
		Enabled:       true,
		AdminUsername: "  admin  ",
		AdminPassword: "correct-horse-battery-staple",
		TokenSecret:   strings.Repeat("s", 32),
		TokenTTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Login("admin", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("trimmed configured username could not log in: %v", err)
	}
}

func TestAuthServiceRejectsExpiredToken(t *testing.T) {
	service, err := NewAuthService(AuthConfig{
		Enabled:       true,
		AdminUsername: "admin",
		AdminPassword: "correct-horse-battery-staple",
		TokenSecret:   strings.Repeat("s", 32),
		TokenTTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.sign(Claims{Subject: "admin", Role: RoleAdmin, IssuedAt: time.Now().Add(-time.Hour).Unix(), ExpiresAt: time.Now().Add(-time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(token); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestResourcePolicyConfinesFilesAndRemotes(t *testing.T) {
	allowedRoot := t.TempDir()
	outsideRoot := t.TempDir()
	insideFile := filepath.Join(allowedRoot, "inside.txt")
	outsideFile := filepath.Join(outsideRoot, "outside.txt")
	if err := os.WriteFile(insideFile, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := NewResourcePolicy(true, []string{allowedRoot}, []string{"backup"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ValidateLocalFile(insideFile); err != nil {
		t.Fatalf("inside file rejected: %v", err)
	}
	if _, err := policy.ValidateLocalFile(outsideFile); err == nil {
		t.Fatal("outside file must be rejected")
	}
	if got, err := policy.ValidateRemote("backup", "/daily/f.txt"); err != nil || got != "daily/f.txt" {
		t.Fatalf("allowed remote got=%q err=%v", got, err)
	}
	if _, err := policy.ValidateRemote("other", "daily/f.txt"); err == nil {
		t.Fatal("non-allowlisted remote must be rejected")
	}
	if _, err := policy.ValidateRemote("backup", "../../secret"); err == nil {
		t.Fatal("remote traversal must be rejected")
	}
}
