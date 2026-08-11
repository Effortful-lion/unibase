package id

import (
	"regexp"
	"testing"
	"time"
)

// ======================== Nanoid ========================

func TestNanoid_DefaultLength(t *testing.T) {
	s := MustNanoid(0)
	// MustNanoid(0) 使用默认长度 21
	if len(s) != 21 {
		t.Fatalf("MustNanoid(0) length = %d, want 21", len(s))
	}
}

func TestNanoid_SpecifiedLength(t *testing.T) {
	for _, size := range []int{1, 8, 32, 64} {
		s, err := Nanoid(size)
		if err != nil {
			t.Fatalf("Nanoid(%d) error: %v", size, err)
		}
		if len(s) != size {
			t.Fatalf("Nanoid(%d) length = %d, want %d", size, len(s), size)
		}
	}
}

func TestNanoid_URLSafe(t *testing.T) {
	s, _ := Nanoid(32)
	// go-nanoid 默认字符集是 URL-safe 的
	for _, c := range s {
		if !isNanoidChar(c) {
			t.Fatalf("Nanoid contains non-alphabet character: %c in %s", c, s)
		}
	}
}

func TestNanoid_DifferentOutputs(t *testing.T) {
	s1, _ := Nanoid(21)
	s2, _ := Nanoid(21)
	if s1 == s2 {
		t.Fatal("two consecutive Nanoid(21) calls produced identical output")
	}
}

func TestNanoid_NegativePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative size with MustNanoid")
		}
	}()
	MustNanoid(-1)
}

func TestNanoid_MustNanoid(t *testing.T) {
	s := MustNanoid(21)
	if len(s) != 21 {
		t.Fatalf("MustNanoid length = %d, want 21", len(s))
	}
}

func isNanoidChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '-'
}

// ======================== UUID ========================

func TestUUID_Format(t *testing.T) {
	u := UUID()
	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	matched, err := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, u)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("UUID format invalid: %s", u)
	}
}

func TestUUID_Unique(t *testing.T) {
	u1 := UUID()
	u2 := UUID()
	if u1 == u2 {
		t.Fatal("two consecutive UUID() calls produced identical output")
	}
}

// ======================== Snowflake ========================

func TestSnowflake_ValidWorkerID(t *testing.T) {
	_, err := NewSnowflake(0)
	if err != nil {
		t.Fatalf("NewSnowflake(0) error: %v", err)
	}
	_, err = NewSnowflake(1023)
	if err != nil {
		t.Fatalf("NewSnowflake(1023) error: %v", err)
	}
}

func TestSnowflake_InvalidWorkerID(t *testing.T) {
	_, err := NewSnowflake(-1)
	if err == nil {
		t.Fatal("expected error for workerID=-1")
	}
	_, err = NewSnowflake(1024)
	if err == nil {
		t.Fatal("expected error for workerID=1024")
	}
}

func TestSnowflake_GeneratedIDsUnique(t *testing.T) {
	sf, _ := NewSnowflake(1)
	ids := make(map[int64]struct{})
	for i := 0; i < 10000; i++ {
		id := sf.Generate()
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate snowflake ID generated: %d", id)
		}
		ids[id] = struct{}{}
	}
}

func TestSnowflake_MonotonicallyIncreasing(t *testing.T) {
	sf, _ := NewSnowflake(1)
	var prev int64
	for i := 0; i < 1000; i++ {
		id := sf.Generate()
		if id <= prev {
			t.Fatalf("snowflake ID not monotonically increasing: %d <= %d", id, prev)
		}
		prev = id
	}
}

func TestSnowflake_DifferentWorkers(t *testing.T) {
	sf1, _ := NewSnowflake(1)
	sf2, _ := NewSnowflake(2)

	id1 := sf1.Generate()
	id2 := sf2.Generate()

	// 同一毫秒生成的 ID 应该不同（不同 worker ID）
	if id1 == id2 {
		t.Fatal("different workers produced identical snowflake IDs")
	}

	// 不同 worker 的 ID 可以比较大小（时间戳部分相同，worker ID 不同）
	// 不做严格断言，只要不同即可
}

func TestSnowflake_TimestampComponent(t *testing.T) {
	before := time.Now().UnixMilli()
	sf, _ := NewSnowflake(0)
	id := sf.Generate()
	after := time.Now().UnixMilli()

	// bwmarrin/snowflake 使用 Twitter epoch (1288834974657)
	// 从 ID 提取时间戳：高 41 位是自 epoch 以来的毫秒数
	ts := (id >> 22) + 1288834974657

	if ts < before || ts > after+1000 {
		t.Fatalf("snowflake timestamp %d out of range [%d, %d]", ts, before, after+1000)
	}
}

func TestSnowflake_Must(t *testing.T) {
	// 验证 MustNanoid 可用（nanoid.go 中定义）
	s := MustNanoid(10)
	if len(s) != 10 {
		t.Fatalf("MustNanoid length = %d, want 10", len(s))
	}
}
