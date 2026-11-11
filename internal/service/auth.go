package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// 认证相关错误。
var (
	ErrBadCredentials = errors.New("用户名或密码错误")
	ErrLocked         = errors.New("失败次数过多，请稍后再试")
	ErrBadToken       = errors.New("登录已失效，请重新登录")
)

// AuthService 单用户认证：口令来自配置，签发 HS256 JWT。
type AuthService struct {
	username string
	password string
	hash     []byte
	bootID   string
	secret   []byte
	ttl      time.Duration

	mu       sync.Mutex
	failures map[string]*failure
}

type failure struct {
	n     int
	until time.Time
	last  time.Time
}

const (
	maxFailures = 5
	lockFor     = 5 * time.Minute
)

type authClaims struct {
	BootID string `json:"boot_id"`
	jwt.RegisteredClaims
}

// NewAuthService 创建认证服务。
func NewAuthService(username, password, passwordHash string, secret []byte, ttl time.Duration) *AuthService {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	boot := make([]byte, 32)
	if _, err := rand.Read(boot); err != nil {
		panic("生成登录会话标识失败")
	}
	a := &AuthService{bootID: hex.EncodeToString(boot), username: username, password: password, secret: secret, ttl: ttl, failures: map[string]*failure{}}
	if passwordHash != "" {
		a.hash = []byte(passwordHash)
	}
	return a
}

// ResolveSecret 优先用配置里的密钥；否则从 dataDir/.jwt_key 读取，没有就生成并落盘（0600）。
// 签名密钥保持稳定；启动时另生成 bootID，让旧会话在重启后失效。
func ResolveSecret(configured, dataDir string) ([]byte, error) {
	if configured != "" {
		return []byte(configured), nil
	}
	path := filepath.Join(dataDir, ".jwt_key")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return []byte(strings.TrimSpace(string(b))), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	key := hex.EncodeToString(buf)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return nil, fmt.Errorf("写入 JWT 密钥: %w", err)
	}
	return []byte(key), nil
}

// Login 校验口令并签发 token。clientIP 用于失败计数（防暴力破解）。
func (a *AuthService) Login(username, password, clientIP string) (string, time.Time, error) {
	now := time.Now()
	a.mu.Lock()
	f := a.failures[clientIP]
	if f != nil && now.Before(f.until) {
		a.mu.Unlock()
		return "", time.Time{}, ErrLocked
	}
	a.mu.Unlock()

	if !a.check(username, password) {
		a.mu.Lock()
		if f == nil || now.Sub(f.last) > lockFor {
			f = &failure{}
			a.failures[clientIP] = f
		}
		f.n++
		f.last = now
		if f.n >= maxFailures {
			f.until = now.Add(lockFor)
			f.n = 0
		}
		if len(a.failures) > 10_000 {
			a.failures = map[string]*failure{}
		}
		a.mu.Unlock()
		return "", time.Time{}, ErrBadCredentials
	}
	a.mu.Lock()
	delete(a.failures, clientIP)
	a.mu.Unlock()

	exp := now.Add(a.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{BootID: a.bootID, RegisteredClaims: jwt.RegisteredClaims{
		Subject:   a.username,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		Issuer:    "ipguard",
	}})
	s, err := tok.SignedString(a.secret)
	return s, exp, err
}

func (a *AuthService) check(username, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	var passOK bool
	if a.hash != nil {
		passOK = bcrypt.CompareHashAndPassword(a.hash, []byte(password)) == nil
	} else {
		passOK = subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
	}
	return userOK && passOK
}

// Verify 校验 token，返回用户名。
func (a *AuthService) Verify(token string) (string, error) {
	claims := &authClaims{}
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrBadToken
		}
		return a.secret, nil
	}, jwt.WithIssuer("ipguard"), jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid || claims.Subject != a.username || claims.BootID == "" || claims.BootID != a.bootID {
		return "", ErrBadToken
	}
	return claims.Subject, nil
}
