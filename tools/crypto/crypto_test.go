package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// ======================== AES-GCM ========================

func TestAESGCM_Roundtrip(t *testing.T) {
	key := make([]byte, 32) // AES-256
	_, err := rand.Read(key)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("hello, aes-gcm world! 你好世界")
	ciphertext, err := AESGCMEncrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := AESGCMDecrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("roundtrip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestAESGCM_KeyLengths(t *testing.T) {
	plaintext := []byte("test data")
	for _, keyLen := range []int{16, 24, 32} {
		key := make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		ct, err := AESGCMEncrypt(plaintext, key)
		if err != nil {
			t.Fatalf("AES-%d encrypt failed: %v", keyLen*8, err)
		}
		pt, err := AESGCMDecrypt(ct, key)
		if err != nil {
			t.Fatalf("AES-%d decrypt failed: %v", keyLen*8, err)
		}
		if !bytes.Equal(pt, plaintext) {
			t.Fatalf("AES-%d roundtrip mismatch", keyLen*8)
		}
	}
}

func TestAESGCM_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	ct, _ := AESGCMEncrypt([]byte("secret"), key1)
	_, err := AESGCMDecrypt(ct, key2)
	if err == nil {
		t.Fatal("expected error with wrong key, got nil")
	}
}

func TestAESGCM_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	ct, _ := AESGCMEncrypt([]byte("secret"), key)
	// 篡改密文最后一个字节
	ct[len(ct)-1] ^= 0xFF

	_, err := AESGCMDecrypt(ct, key)
	if err == nil {
		t.Fatal("expected error with tampered ciphertext, got nil")
	}
}

func TestAESGCM_TooShortCiphertext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	_, err := AESGCMDecrypt([]byte("short"), key)
	if err != ErrCiphertextTooShort {
		t.Fatalf("expected ErrCiphertextTooShort, got: %v", err)
	}
}

func TestAESGCM_EmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	ct, err := AESGCMEncrypt(nil, key)
	if err != nil {
		t.Fatalf("encrypt empty failed: %v", err)
	}
	pt, err := AESGCMDecrypt(ct, key)
	if err != nil {
		t.Fatalf("decrypt empty failed: %v", err)
	}
	if pt == nil {
		pt = []byte{}
	}
	if len(pt) != 0 {
		t.Fatalf("expected empty plaintext, got %q", pt)
	}
}

// ======================== ECDH ========================

func TestECDH_KeyGeneration(t *testing.T) {
	priv, err := GenerateECDHKey()
	if err != nil {
		t.Fatalf("GenerateECDHKey failed: %v", err)
	}
	if priv == nil || priv.PublicKey.Curve == nil {
		t.Fatal("generated key is nil or invalid")
	}
}

func TestECDH_MarshalParsePublicKey(t *testing.T) {
	priv, _ := GenerateECDHKey()
	pub := &priv.PublicKey

	b64, err := MarshalPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPublicKey failed: %v", err)
	}

	parsed, err := ParsePublicKey(b64)
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}

	if !pub.Equal(parsed) {
		t.Fatal("public key mismatch after marshal/parse roundtrip")
	}
}

func TestECDH_MarshalParsePrivateKey(t *testing.T) {
	priv, _ := GenerateECDHKey()

	b64, err := MarshalPrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPrivateKey failed: %v", err)
	}

	parsed, err := ParsePrivateKey(b64)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}

	if !priv.Equal(parsed) {
		t.Fatal("private key mismatch after marshal/parse roundtrip")
	}
}

func TestECDH_PublicKeyPEM(t *testing.T) {
	priv, _ := GenerateECDHKey()
	pemStr, err := MarshalPublicKeyPEM(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPublicKeyPEM failed: %v", err)
	}
	if len(pemStr) == 0 {
		t.Fatal("PEM output is empty")
	}
	// PEM 格式包含 BEGIN/END 标记
	if !bytes.Contains([]byte(pemStr), []byte("BEGIN PUBLIC KEY")) ||
		!bytes.Contains([]byte(pemStr), []byte("END PUBLIC KEY")) {
		t.Fatalf("PEM format invalid: %s", pemStr)
	}
}

func TestECDH_PrivateKeyPEM(t *testing.T) {
	priv, _ := GenerateECDHKey()
	pemStr, err := MarshalPrivateKeyPEM(priv)
	if err != nil {
		t.Fatalf("MarshalPrivateKeyPEM failed: %v", err)
	}
	if len(pemStr) == 0 {
		t.Fatal("PEM output is empty")
	}
	if !bytes.Contains([]byte(pemStr), []byte("BEGIN PRIVATE KEY")) ||
		!bytes.Contains([]byte(pemStr), []byte("END PRIVATE KEY")) {
		t.Fatalf("PEM format invalid: %s", pemStr)
	}
}

func TestECDH_SharedSecret(t *testing.T) {
	serverPriv, _ := GenerateECDHKey()
	clientPriv, _ := GenerateECDHKey()

	serverPubBytes, _ := MarshalPublicKey(&serverPriv.PublicKey)
	clientPubBytes, _ := MarshalPublicKey(&clientPriv.PublicKey)

	serverPub, _ := ParsePublicKey(serverPubBytes)
	clientPub, _ := ParsePublicKey(clientPubBytes)

	serverSecret, err := ComputeSharedSecret(clientPub, serverPriv)
	if err != nil {
		t.Fatalf("server ComputeSharedSecret failed: %v", err)
	}

	clientSecret, err := ComputeSharedSecret(serverPub, clientPriv)
	if err != nil {
		t.Fatalf("client ComputeSharedSecret failed: %v", err)
	}

	if !bytes.Equal(serverSecret, clientSecret) {
		t.Fatal("shared secrets do not match")
	}
}

func TestECDH_DeriveKey(t *testing.T) {
	priv, _ := GenerateECDHKey()
	pubBytes, _ := MarshalPublicKey(&priv.PublicKey)
	pub, _ := ParsePublicKey(pubBytes)

	secret, err := ComputeSharedSecret(pub, priv)
	if err != nil {
		t.Fatalf("ComputeSharedSecret failed: %v", err)
	}

	key := DeriveKey(secret)
	if len(key) != 32 {
		t.Fatalf("DeriveKey output length = %d, want 32", len(key))
	}
}

func TestECDH_CompleteFlow(t *testing.T) {
	// 模拟完整的密钥交换 → 派生 → AES-GCM 加解密流程
	serverPriv, _ := GenerateECDHKey()
	clientPriv, _ := GenerateECDHKey()

	serverPubBytes, _ := MarshalPublicKey(&serverPriv.PublicKey)
	clientPubBytes, _ := MarshalPublicKey(&clientPriv.PublicKey)

	// 服务端计算共享密钥
	serverPub, _ := ParsePublicKey(clientPubBytes)
	serverSecret, _ := ComputeSharedSecret(serverPub, serverPriv)
	serverKey := DeriveKey(serverSecret)

	// 客户端计算共享密钥
	clientPub, _ := ParsePublicKey(serverPubBytes)
	clientSecret, _ := ComputeSharedSecret(clientPub, clientPriv)
	clientKey := DeriveKey(clientSecret)

	if !bytes.Equal(serverKey, clientKey) {
		t.Fatal("derived keys do not match")
	}

	// 用派生密钥做 AES-GCM 加解密
	plaintext := []byte("end-to-end encrypted message")
	ct, err := AESGCMEncrypt(plaintext, serverKey)
	if err != nil {
		t.Fatalf("AESGCMEncrypt failed: %v", err)
	}
	pt, err := AESGCMDecrypt(ct, clientKey)
	if err != nil {
		t.Fatalf("AESGCMDecrypt failed: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatal("E2E encrypted plaintext mismatch")
	}
}

func TestECDH_InvalidPublicKey(t *testing.T) {
	_, err := ParsePublicKey("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestECDH_InvalidPrivateKey(t *testing.T) {
	_, err := ParsePrivateKey("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

// ======================== Password (bcrypt) ========================

func TestPassword_HashAndVerify(t *testing.T) {
	password := "myStr0ngP@ssword!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if len(hash) == 0 {
		t.Fatal("hash is empty")
	}

	if !VerifyPassword(hash, password) {
		t.Fatal("expected VerifyPassword to return true for correct password")
	}

	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("expected VerifyPassword to return false for wrong password")
	}
}

func TestPassword_DifferentHashes(t *testing.T) {
	// bcrypt 每次生成不同的 hash（随机 salt），但都能验证同一个密码
	password := "same-password"
	hash1, _ := HashPassword(password)
	hash2, _ := HashPassword(password)

	if hash1 == hash2 {
		t.Fatal("expected different hashes due to random salt")
	}

	if !VerifyPassword(hash1, password) {
		t.Fatal("expected hash1 to verify")
	}
	if !VerifyPassword(hash2, password) {
		t.Fatal("expected hash2 to verify")
	}
}

// ======================== Hash (Sha256 / MD5) ========================

func TestSha256_KnownValue(t *testing.T) {
	got := Sha256Hex([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("Sha256Hex mismatch: got %s, want %s", got, want)
	}
}

func TestMD5_KnownValue(t *testing.T) {
	got := MD5Hex([]byte("hello"))
	want := "5d41402abc4b2a76b9719d911017c592"
	if got != want {
		t.Fatalf("MD5Hex mismatch: got %s, want %s", got, want)
	}
}
