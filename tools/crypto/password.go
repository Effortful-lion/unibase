package crypto

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword 使用 bcrypt 哈希密码。
// 自动生成随机 salt，无需外部处理。返回值可直接存入数据库。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword 验证密码与 bcrypt hash 是否匹配。
// 用于登录/校验场景，返回 true 表示密码正确。
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
