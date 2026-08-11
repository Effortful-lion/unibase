package random

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// String 使用 crypto/rand 生成 n 字节的随机字符串（hex 编码，长度为 2n）。
// 用于生成 token、密钥片段等需要密码学安全随机性的场景。
func String(n int) (string, error) {
	if n < 0 {
		return "", errors.New("random: n must be non-negative")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// MustString 同 String，但 panic 于错误（仅用于初始化等不可能失败的场景）。
func MustString(n int) string {
	s, err := String(n)
	if err != nil {
		panic(err)
	}
	return s
}

// Base64 使用 crypto/rand 生成 n 字节的随机字符串（URL-safe base64）。
// 用于生成 session token、验证码等需要 URL 安全传输的场景。
func Base64(n int) (string, error) {
	if n < 0 {
		return "", errors.New("random: n must be non-negative")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// MustBase64 同 Base64，但 panic 于错误。
func MustBase64(n int) string {
	s, err := Base64(n)
	if err != nil {
		panic(err)
	}
	return s
}

// Bytes 生成 n 字节的密码学安全随机字节切片。
// 用于生成 AES key、nonce、salt 等原始随机数据。
func Bytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("random: n must be non-negative")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MustBytes 同 Bytes，但 panic 于错误。
func MustBytes(n int) []byte {
	b, err := Bytes(n)
	if err != nil {
		panic(err)
	}
	return b
}
