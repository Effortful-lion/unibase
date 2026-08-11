package hash

import (
	"encoding/hex"
	"testing"
)

func TestSha256_KnownValue(t *testing.T) {
	got := Sha256Hex([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("Sha256Hex mismatch: got %s, want %s", got, want)
	}
}

func TestSha256_Empty(t *testing.T) {
	got := Sha256Hex(nil)
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("Sha256Hex empty mismatch: got %s, want %s", got, want)
	}
}

func TestMD5_KnownValue(t *testing.T) {
	got := MD5Hex([]byte("hello"))
	want := "5d41402abc4b2a76b9719d911017c592"
	if got != want {
		t.Fatalf("MD5Hex mismatch: got %s, want %s", got, want)
	}
}

func TestMD5_Empty(t *testing.T) {
	got := MD5Hex(nil)
	want := "d41d8cd98f00b204e9800998ecf8427e"
	if got != want {
		t.Fatalf("MD5Hex empty mismatch: got %s, want %s", got, want)
	}
}

func TestCRC32_KnownValue(t *testing.T) {
	got := CRC32Hex([]byte("hello"))
	want := "3610a686"
	if got != want {
		t.Fatalf("CRC32Hex mismatch: got %s, want %s", got, want)
	}
}

func TestCRC32_Empty(t *testing.T) {
	got := CRC32Hex(nil)
	want := "00000000"
	if got != want {
		t.Fatalf("CRC32Hex empty mismatch: got %s, want %s", got, want)
	}
}

func TestCRC32_Length(t *testing.T) {
	b := CRC32([]byte("test data"))
	if len(b) != 4 {
		t.Fatalf("CRC32 length = %d, want 4", len(b))
	}
}

func TestSha256_RawBytes(t *testing.T) {
	b := Sha256([]byte("hello"))
	if len(b) != 32 {
		t.Fatalf("Sha256 raw length = %d, want 32", len(b))
	}
	if hex.EncodeToString(b) != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal("Sha256 raw bytes mismatch")
	}
}

func TestMD5_RawBytes(t *testing.T) {
	b := MD5([]byte("hello"))
	if len(b) != 16 {
		t.Fatalf("MD5 raw length = %d, want 16", len(b))
	}
	if hex.EncodeToString(b) != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatal("MD5 raw bytes mismatch")
	}
}
