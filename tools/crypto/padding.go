package crypto

import (
	"bytes"
	"errors"
)

// pkcs7Padding 对数据进行 PKCS7 填充。
// blockSize 通常为 16（AES 块大小）。
func pkcs7Padding(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padtext...)
}

// pkcs7UnPadding 去除 PKCS7 填充。
func pkcs7UnPadding(src []byte, blockSize int) ([]byte, error) {
	if len(src)%blockSize != 0 {
		return nil, errors.New("crypto: padding size error")
	}
	padding := int(src[len(src)-1])
	if padding > blockSize || padding == 0 {
		return nil, errors.New("crypto: padding size error")
	}
	for i := len(src) - 1; i > len(src)-1-padding; i-- {
		if src[i] != byte(padding) {
			return nil, errors.New("crypto: padding content error")
		}
	}
	return src[:len(src)-padding], nil
}
