package basic

import (
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	tests := []struct {
		username string
		password string
	}{
		{"admin", "password123"},
		{"", ""},
		{"user", "中文密码"},
	}

	for _, tc := range tests {
		encoded := Encode(tc.username, tc.password)
		username, password, ok := Decode(encoded)
		if !ok {
			t.Errorf("Decode failed for (%q, %q)", tc.username, tc.password)
			continue
		}
		if username != tc.username || password != tc.password {
			t.Errorf("roundtrip mismatch: got (%q, %q), want (%q, %q)",
				username, password, tc.username, tc.password)
		}
	}
}

func TestDecode_InvalidHeader(t *testing.T) {
	tests := []string{
		"",
		"Bearer token123",
		"Basic ",
		"Basic notbase64!!!",
	}

	for _, header := range tests {
		_, _, ok := Decode(header)
		if ok {
			t.Errorf("Decode should fail for %q", header)
		}
	}
}

func TestBcryptAuthenticator(t *testing.T) {
	hash, err := HashPassword("mypassword")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	auth := NewBcryptAuthenticator(map[string]string{
		"alice": hash,
	})

	if !auth.Authenticate("alice", "mypassword") {
		t.Error("expected authenticate success")
	}
	if auth.Authenticate("alice", "wrongpassword") {
		t.Error("expected authenticate failure")
	}
	if auth.Authenticate("unknown", "anything") {
		t.Error("expected authenticate failure for unknown user")
	}
}

func TestHashPassword_UniqueHashes(t *testing.T) {
	hash1, _ := HashPassword("samepassword")
	hash2, _ := HashPassword("samepassword")

	if hash1 == hash2 {
		t.Error("two bcrypt hashes of the same password should differ (random salt)")
	}

	if !CompareBcrypt("samepassword", hash1) {
		t.Error("first hash should match original password")
	}
	if !CompareBcrypt("samepassword", hash2) {
		t.Error("second hash should match original password")
	}
}
