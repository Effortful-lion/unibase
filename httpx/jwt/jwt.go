package jwt

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// Parser JWT 处理器，管理密钥、解析和签名逻辑。
// 提供 Parse(解析验证)、Sign(签名生成)、Verify(快速验证) 三个核心方法。
type Parser struct {
	signingMethod jwt.SigningMethod
	keyFunc       func(*jwt.Token) (interface{}, error)
}

// NewParser 创建 JWT 解析器。
// keyFunc 用于从 token 中提取签名验证密钥（支持密钥轮转：根据 token header 的 kid 选择密钥）。
func NewParser(signingMethod jwt.SigningMethod, keyFunc func(*jwt.Token) (interface{}, error)) *Parser {
	return &Parser{
		signingMethod: signingMethod,
		keyFunc:       keyFunc,
	}
}

// Parse 解析并验证 JWT token，返回 Claims。
func (p *Parser) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, p.keyFunc, jwt.WithValidMethods([]string{p.signingMethod.Alg()}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid claims type")
	}
	return claims, nil
}

// Sign 用指定密钥签名 Claims，返回 JWT token 字符串。
func (p *Parser) Sign(claims Claims, key interface{}) (string, error) {
	token := jwt.NewWithClaims(p.signingMethod, &claims)
	return token.SignedString(key)
}

// Verify 验证 token 是否有效（签名正确、未过期）。
func (p *Parser) Verify(tokenString string) bool {
	_, err := p.Parse(tokenString)
	return err == nil
}

// NewHMACParser 创建 HMAC 签名算法的解析器（最常用场景）。
func NewHMACParser(secret []byte) *Parser {
	return NewParser(jwt.SigningMethodHS256, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
}
