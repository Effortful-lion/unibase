package jwt

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Effortful-lion/unibase/tools/auth/jwt"
	gjwt "github.com/golang-jwt/jwt/v5"
)

// --- 内部错误 ---

var (
	errInvalidKey        = errors.New("jwt: signing key must be []byte")
	errTokenNotFound     = errors.New("jwt: token not found")
	errInvalidClaimsType = errors.New("jwt: invalid claims type")
)

// Claims 是 tools/auth/jwt.Claims 的快捷方式，携带 JSON tags，适用于 HTTP 响应序列化。
// 包含 Extra 扩展字段，可通过 Set/Get 方法读写自定义数据。
type Claims = jwt.Claims

// NewClaims 创建标准 Claims（不含时间戳，由 Manager 签发时自动填充）。
// ttl 用于生成 token 时设置过期时间，不改变 claims 本身。
func NewClaims(userID, username string, ttl time.Duration) Claims {
	c := jwt.NewClaims(userID, username)
	// 在 HTTP 场景中，用户可能直接需要带过期时间的 claims
	c.ExpiresAt = gjwt.NewNumericDate(time.Now().Add(ttl))
	c.IssuedAt = gjwt.NewNumericDate(time.Now())
	return *c
}

// NewMapClaims 从 map 创建 MapClaims，自动填充 exp 和 iat。
// data 中的键值对直接写入 token payload。
func NewMapClaims(data map[string]any, ttl time.Duration) gjwt.MapClaims {
	now := time.Now()
	mc := gjwt.MapClaims(data)
	mc["exp"] = now.Add(ttl).Unix()
	mc["iat"] = now.Unix()
	return mc
}

// Parser JWT 处理器，基于 tools/auth/jwt.Manager 实现。
type Parser struct {
	mgr    jwt.Manager
	secret []byte
}

// NewHMACParser 创建 HMAC 签名算法的解析器（最常用场景）。
func NewHMACParser(secret []byte) *Parser {
	return &Parser{
		mgr:    jwt.NewManager(string(secret)),
		secret: secret,
	}
}

// Generate 用任意实现 gjwt.Claims 的类型生成 JWT token。
// 支持：Claims、MapClaims、自定义 struct。
func (p *Parser) Generate(claims gjwt.Claims, key any) (string, error) {
	secret, ok := key.([]byte)
	if !ok {
		return "", errInvalidKey
	}
	return jwt.NewManager(string(secret)).GenerateWithClaims(claims)
}

// Sign 用标准 Claims 签名，返回 JWT token 字符串。
func (p *Parser) Sign(claims Claims, key any) (string, error) {
	secret, ok := key.([]byte)
	if !ok {
		return "", errInvalidKey
	}
	return jwt.NewManager(string(secret)).GenerateWithClaims(claims)
}

// Parse 解析并验证 JWT token，返回标准 Claims。
func (p *Parser) Parse(tokenString string) (*Claims, error) {
	return p.mgr.Parse(tokenString)
}

// ParseMap 解析并验证 JWT token，返回 MapClaims。
func (p *Parser) ParseMap(tokenString string) (gjwt.MapClaims, error) {
	token, err := gjwt.Parse(tokenString, func(token *gjwt.Token) (interface{}, error) {
		return p.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(gjwt.MapClaims)
	if !ok {
		return nil, errInvalidClaimsType
	}
	if !token.Valid {
		return nil, jwt.ErrInvalidToken
	}
	return claims, nil
}

// Verify 验证 token 是否有效（签名正确、未过期）。
func (p *Parser) Verify(tokenString string) bool {
	_, err := p.mgr.Parse(tokenString)
	return err == nil
}

// --- 内部函数 ---

func extractToken(req *http.Request) (string, error) {
	token := req.Header.Get("Authorization")
	if token != "" {
		token = trimBearer(token)
		if token != "" {
			return token, nil
		}
	}

	token = req.URL.Query().Get("token")
	if token != "" {
		return token, nil
	}

	return "", errTokenNotFound
}

func trimBearer(s string) string {
	s = strings.TrimPrefix(s, "Bearer ")
	s = strings.TrimPrefix(s, "bearer ")
	return strings.TrimSpace(s)
}
