// Package basic 提供 HTTP Basic Auth 的编码、解码与校验能力。
//
// 密码校验委托给 Authenticator 接口，调用方传入基于 bcrypt 的实现，
// 确保密码在存储和校验过程中始终以哈希形式处理，不会明文泄露。
package basic

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/Effortful-lion/unibase/tools/crypto"
)

// Credentials 表示 Basic Auth 的用户名和密码。
type Credentials struct {
	Username string
	Password string
}

// Authenticator 定义了用户名和密码的校验逻辑。
//
// 典型实现使用 bcrypt 存储密码哈希，Authenticate 时调用
// bcrypt.CompareHashAndPassword 进行比对。
type Authenticator interface {
	Authenticate(username, password string) bool
}

// Encode 将用户名和密码编码为 HTTP Basic Auth 头值。
// 返回格式为 "Basic base64(username:password)"。
func Encode(username, password string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Basic " + credentials
}

// Decode 从 HTTP Authorization 头中解析 Basic Auth 凭证。
// 返回 (username, password, true) 或 ("", "", false)。
func Decode(authHeader string) (string, string, bool) {
	if authHeader == "" {
		return "", "", false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Basic" {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}

	credParts := strings.SplitN(string(decoded), ":", 2)
	if len(credParts) != 2 {
		return "", "", false
	}

	return credParts[0], credParts[1], true
}

// bcryptAuthenticator 是基于 bcrypt 的 Authenticator 实现。
type bcryptAuthenticator struct {
	// passwordHashes 存储用户名到 bcrypt 哈希的映射。
	// 生产环境中应替换为数据库查询。
	passwordHashes map[string]string
}

// NewBcryptAuthenticator 创建基于 bcrypt 哈希映射的校验器。
//
// passwordHashes 的 key 为用户名，value 为 bcrypt 生成的哈希串。
// 注册用户时使用 HashPassword 生成哈希后存入此映射或持久化到数据库。
func NewBcryptAuthenticator(passwordHashes map[string]string) Authenticator {
	return &bcryptAuthenticator{passwordHashes: passwordHashes}
}

func (a *bcryptAuthenticator) Authenticate(username, password string) bool {
	hash, ok := a.passwordHashes[username]
	if !ok {
		return false
	}
	return CompareBcrypt(password, hash)
}

// HashPassword 使用 bcrypt 对明文密码进行哈希，供注册时使用。
// 委托给 crypto.HashPassword 实现，成本固定为 bcrypt.DefaultCost。
func HashPassword(password string) (string, error) {
	return crypto.HashPassword(password)
}

// CompareBcrypt 比较明文密码与 bcrypt 哈希。
// 返回 true 表示匹配。委托给 crypto.VerifyPassword 实现。
func CompareBcrypt(password, hash string) bool {
	return crypto.VerifyPassword(hash, password)
}

// ErrInvalidCredentials 表示用户名不存在或密码不匹配。
var ErrInvalidCredentials = errors.New("basic: invalid username or password")
