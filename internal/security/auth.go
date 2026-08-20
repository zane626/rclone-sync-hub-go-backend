package security

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type AuthConfig struct {
	Enabled        bool
	AdminUsername  string
	AdminPassword  string
	ViewerUsername string
	ViewerPassword string
	TokenSecret    string
	TokenTTL       time.Duration
}

type Claims struct {
	Subject   string `json:"sub"`
	Role      string `json:"role"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	TokenID   string `json:"jti"`
}

type credential struct {
	passwordHash []byte
	role         string
}

type AuthService struct {
	enabled     bool
	credentials map[string]credential
	secret      []byte
	tokenTTL    time.Duration
}

func NewAuthService(config AuthConfig) (*AuthService, error) {
	service := &AuthService{enabled: config.Enabled, credentials: make(map[string]credential), tokenTTL: config.TokenTTL}
	if !config.Enabled {
		return service, nil
	}
	if len(config.AdminPassword) < 12 {
		return nil, errors.New("AUTH_ADMIN_PASSWORD must contain at least 12 characters")
	}
	if len(config.TokenSecret) < 32 {
		return nil, errors.New("AUTH_TOKEN_SECRET must contain at least 32 characters")
	}
	if config.AdminUsername == "" {
		config.AdminUsername = "admin"
	}
	if config.TokenTTL <= 0 {
		config.TokenTTL = 12 * time.Hour
	}
	service.secret = []byte(config.TokenSecret)
	service.tokenTTL = config.TokenTTL
	if err := service.addCredential(config.AdminUsername, config.AdminPassword, RoleAdmin); err != nil {
		return nil, err
	}
	if config.ViewerUsername != "" || config.ViewerPassword != "" {
		if config.ViewerUsername == "" || len(config.ViewerPassword) < 12 {
			return nil, errors.New("viewer username and a password of at least 12 characters must both be configured")
		}
		if err := service.addCredential(config.ViewerUsername, config.ViewerPassword, RoleViewer); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func (s *AuthService) Enabled() bool {
	return s != nil && s.enabled
}

func (s *AuthService) Login(username, password string) (string, Claims, error) {
	if !s.Enabled() {
		return "", Claims{}, errors.New("authentication is disabled")
	}
	entry, ok := s.credentials[username]
	if !ok || bcrypt.CompareHashAndPassword(entry.passwordHash, []byte(password)) != nil {
		// Perform a bcrypt comparison even for unknown users to reduce username timing leakage.
		if !ok {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		}
		return "", Claims{}, ErrInvalidCredentials
	}
	now := time.Now()
	claims := Claims{
		Subject:   username,
		Role:      entry.role,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(s.tokenTTL).Unix(),
		TokenID:   randomTokenID(),
	}
	token, err := s.sign(claims)
	return token, claims, err
}

func (s *AuthService) Verify(token string) (Claims, error) {
	if !s.Enabled() {
		return Claims{Subject: "anonymous", Role: RoleAdmin, ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
	}
	if len(token) > 4096 {
		return Claims{}, errors.New("invalid access token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return Claims{}, errors.New("invalid access token")
	}
	signedValue := parts[0] + "." + parts[1]
	expected := hmac.New(sha256.New, s.secret)
	_, _ = expected.Write([]byte(signedValue))
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(provided, expected.Sum(nil)) {
		return Claims{}, errors.New("invalid access token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid access token")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, errors.New("invalid access token")
	}
	if claims.Subject == "" || claims.ExpiresAt <= time.Now().Unix() {
		return Claims{}, errors.New("access token expired")
	}
	if claims.Role != RoleAdmin && claims.Role != RoleViewer {
		return Claims{}, errors.New("invalid access token role")
	}
	return claims, nil
}

func (s *AuthService) addCredential(username, password, role string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 255 {
		return errors.New("authentication username must contain 1 to 255 characters")
	}
	if _, exists := s.credentials[username]; exists {
		return fmt.Errorf("duplicate authentication username %q", username)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash authentication password: %w", err)
	}
	s.credentials[username] = credential{passwordHash: hash, role: role}
	return nil
}

func (s *AuthService) sign(claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signedValue := "v1." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(signedValue))
	return signedValue + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func randomTokenID() string {
	buffer := make([]byte, 16)
	if _, err := cryptorand.Read(buffer); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

var dummyPasswordHash = func() []byte {
	hash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-never-valid"), bcrypt.DefaultCost)
	return hash
}()
