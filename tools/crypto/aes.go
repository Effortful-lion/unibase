package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// ErrCiphertextTooShort 密文长度不足以提取 nonce。
var ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")

// AESGCMEncrypt 使用 AES-GCM 加密。
//
// key 必须是 16/24/32 字节（对应 AES-128/192/256）。
// 返回的密文格式为 nonce || ciphertext，调用方无需单独存储 nonce。
func AESGCMEncrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Seal 将 nonce 作为前缀追加到密文前面
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// AESGCMDecrypt 解密 AES-GCM 密文。
// 密文格式必须为 nonce || ciphertext（即 AESGCMEncrypt 的输出）。
func AESGCMDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
