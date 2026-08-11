package random

import (
	"encoding/base64"
	"strings"
	"testing"
)

// ======================== String ========================

func TestString_Length(t *testing.T) {
	for _, n := range []int{0, 1, 16, 32, 64} {
		s, err := String(n)
		if err != nil {
			t.Fatalf("String(%d) error: %v", n, err)
		}
		// hex 编码后长度为 2n
		if len(s) != 2*n {
			t.Fatalf("String(%d) length = %d, want %d", n, len(s), 2*n)
		}
	}
}

func TestString_DifferentOutputs(t *testing.T) {
	s1, _ := String(32)
	s2, _ := String(32)
	if s1 == s2 {
		t.Fatal("two consecutive String(32) calls produced identical output")
	}
}

func TestString_ValidHex(t *testing.T) {
	s, _ := String(16)
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("String output contains non-hex character: %c", c)
		}
	}
}

func TestString_NegativePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative n with MustString")
		}
	}()
	MustString(-1)
}

func TestString_MustString(t *testing.T) {
	// Should not panic for valid input
	s := MustString(16)
	if len(s) != 32 {
		t.Fatalf("MustString length = %d, want 32", len(s))
	}
}

// ======================== Base64 ========================

func TestBase64_Length(t *testing.T) {
	for _, n := range []int{0, 1, 16, 32, 64} {
		s, err := Base64(n)
		if err != nil {
			t.Fatalf("Base64(%d) error: %v", n, err)
		}
		// base64.URLEncoding 编码后长度 = ceil(4n/3)
		expectedLen := base64.URLEncoding.EncodedLen(n)
		if len(s) != expectedLen {
			t.Fatalf("Base64(%d) length = %d, want %d", n, len(s), expectedLen)
		}
	}
}

func TestBase64_URLSafe(t *testing.T) {
	s, _ := Base64(32)
	// URL-safe base64 不应包含 + / 字符
	if strings.ContainsAny(s, "+/") {
		t.Fatalf("Base64 output contains unsafe characters: %s", s)
	}
}

func TestBase64_DifferentOutputs(t *testing.T) {
	s1, _ := Base64(32)
	s2, _ := Base64(32)
	if s1 == s2 {
		t.Fatal("two consecutive Base64(32) calls produced identical output")
	}
}

func TestBase64_Decodable(t *testing.T) {
	s, _ := Base64(32)
	decoded, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("Base64 output is not decodable: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded length = %d, want 32", len(decoded))
	}
}

func TestBase64_NegativePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative n with MustBase64")
		}
	}()
	MustBase64(-1)
}

// ======================== Bytes ========================

func TestBytes_Length(t *testing.T) {
	for _, n := range []int{0, 1, 16, 32, 64, 256} {
		b, err := Bytes(n)
		if err != nil {
			t.Fatalf("Bytes(%d) error: %v", n, err)
		}
		if len(b) != n {
			t.Fatalf("Bytes(%d) length = %d, want %d", n, len(b), n)
		}
	}
}

func TestBytes_DifferentOutputs(t *testing.T) {
	b1, _ := Bytes(32)
	b2, _ := Bytes(32)
	if string(b1) == string(b2) {
		t.Fatal("two consecutive Bytes(32) calls produced identical output")
	}
}

func TestBytes_NonZeroEntropy(t *testing.T) {
	// 连续生成多个字节切片，确保不是全零
	for i := 0; i < 100; i++ {
		b, _ := Bytes(32)
		allZero := true
		for _, v := range b {
			if v != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatal("Bytes produced all-zero output")
		}
	}
}

func TestBytes_NilForNegative(t *testing.T) {
	b, err := Bytes(-1)
	if b != nil {
		t.Fatalf("expected nil for negative n, got %v", b)
	}
	if err == nil {
		t.Fatal("expected error for negative n")
	}
}

func TestBytes_MustBytes(t *testing.T) {
	b := MustBytes(16)
	if len(b) != 16 {
		t.Fatalf("MustBytes length = %d, want 16", len(b))
	}
}

// ======================== Int ========================

func TestInt_Range(t *testing.T) {
	for _, n := range []int{2, 10, 100, 1000} {
		for i := 0; i < 1000; i++ {
			v, err := Int(n)
			if err != nil {
				t.Fatalf("Int(%d) error: %v", n, err)
			}
			if v < 0 || v >= n {
				t.Fatalf("Int(%d) = %d, out of range [0, %d)", n, v, n)
			}
		}
	}
}

func TestInt_EdgeCases(t *testing.T) {
	// Int(1) 永远返回 0
	for i := 0; i < 100; i++ {
		v, err := Int(1)
		if err != nil {
			t.Fatalf("Int(1) error: %v", err)
		}
		if v != 0 {
			t.Fatalf("Int(1) = %d, want 0", v)
		}
	}

	// Int(2) 在 0 和 1 之间分布
	var zeros, ones int
	for i := 0; i < 1000; i++ {
		v, _ := Int(2)
		if v == 0 {
			zeros++
		} else if v == 1 {
			ones++
		} else {
			t.Fatalf("Int(2) = %d, want 0 or 1", v)
		}
	}
	if zeros == 0 || ones == 0 {
		t.Fatalf("Int(2) distribution unexpected: zeros=%d, ones=%d", zeros, ones)
	}
}

func TestInt_InvalidInput(t *testing.T) {
	_, err := Int(0)
	if err == nil {
		t.Fatal("expected error for Int(0), got nil")
	}

	_, err = Int(-1)
	if err == nil {
		t.Fatal("expected error for Int(-1), got nil")
	}
}

func TestInt_MustInt(t *testing.T) {
	v := MustInt(100)
	if v < 0 || v >= 100 {
		t.Fatalf("MustInt(100) = %d, out of range", v)
	}
}

func TestInt_MustIntPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for MustInt(0)")
		}
	}()
	MustInt(0)
}
