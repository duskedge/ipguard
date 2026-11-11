package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthRestartInvalidatesTokens(t *testing.T) {
	key := []byte("same-persistent-signing-key-for-all-boots")
	first := NewAuthService("admin", "password", "", key, time.Hour)
	token, _, err := first.Login("admin", "password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if user, err := first.Verify(token); err != nil || user != "admin" {
		t.Fatalf("当前进程应接受 token: %v", err)
	}
	second := NewAuthService("admin", "password", "", key, time.Hour)
	if _, err := second.Verify(token); err == nil {
		t.Fatal("重启后必须拒绝旧 token，即使签名密钥相同")
	}
	fresh, _, err := second.Login("admin", "password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Verify(fresh); err != nil {
		t.Fatal(err)
	}
	legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "admin", Issuer: "ipguard", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Verify(legacy); err == nil {
		t.Fatal("必须拒绝不含启动标识的旧版 token")
	}
}
