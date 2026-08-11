package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
)

// GenerateECDHKey 生成 ECDSA P-256 密钥对。
func GenerateECDHKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// MarshalPublicKey 将公钥序列化为 base64(DER) 格式（PKIX）。
func MarshalPublicKey(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// ParsePublicKey 解析 base64(DER) 格式的公钥。
func ParsePublicKey(derB64 string) (*ecdsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(derB64)
	if err != nil {
		return nil, err
	}
	pk, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := pk.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("crypto: not an ECDSA public key")
	}
	return pub, nil
}

// MarshalPrivateKey 将私钥序列化为 base64(DER) 格式（PKCS#8）。
func MarshalPrivateKey(priv *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// ParsePrivateKey 解析 base64(DER) 格式的私钥（PKCS#8）。
func ParsePrivateKey(derB64 string) (*ecdsa.PrivateKey, error) {
	der, err := base64.StdEncoding.DecodeString(derB64)
	if err != nil {
		return nil, err
	}
	pk, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	priv, ok := pk.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("crypto: not an ECDSA private key")
	}
	return priv, nil
}

// MarshalPublicKeyPEM 将公钥序列化为 PEM 格式字符串。
func MarshalPublicKeyPEM(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// MarshalPrivateKeyPEM 将私钥序列化为 PEM 格式字符串。
func MarshalPrivateKeyPEM(priv *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// ComputeSharedSecret 使用 ECDH 计算双方共享密钥。
func ComputeSharedSecret(peerPub *ecdsa.PublicKey, priv *ecdsa.PrivateKey) ([]byte, error) {
	peerECDH, err := peerPub.ECDH()
	if err != nil {
		return nil, err
	}
	selfECDH, err := priv.ECDH()
	if err != nil {
		return nil, err
	}
	return selfECDH.ECDH(peerECDH)
}

// DeriveKey 使用 SHA-256 从共享密钥派生固定长度密钥（32 字节）。
// 派生结果可直接用于 AESGCMEncrypt 的 key 参数。
func DeriveKey(sharedSecret []byte) []byte {
	h := sha256.New()
	h.Write(sharedSecret)
	return h.Sum(nil)
}
