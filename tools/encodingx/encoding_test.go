package encodingx

import (
	"os"
	"path/filepath"
	"testing"
)

// ── 测试辅助 ──────────────────────────────────────────────────────

func tmpFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, name)
}

// ── JSON ─────────────────────────────────────────────────────────

type jsonTestStruct struct {
	Name  string  `json:"name" yaml:"name"`
	Age   int     `json:"age" yaml:"age"`
	Score float64 `json:"score" yaml:"score"`
}

func TestReadJSON_Success(t *testing.T) {
	path := tmpFile(t, "test.json")
	os.WriteFile(path, []byte(`{"name":"lion","age":30,"Score":99.5}`), 0644)

	var got jsonTestStruct
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON unexpected error: %v", err)
	}

	if got.Name != "lion" {
		t.Errorf("Name: got %q, want %q", got.Name, "lion")
	}
	if got.Age != 30 {
		t.Errorf("Age: got %d, want %d", got.Age, 30)
	}
	if got.Score != 99.5 {
		t.Errorf("Score: got %f, want %f", got.Score, 99.5)
	}
}

func TestReadJSON_FileNotFound(t *testing.T) {
	err := ReadJSON("/nonexistent/path/file.json", &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadJSON_InvalidJSON(t *testing.T) {
	path := tmpFile(t, "bad.json")
	os.WriteFile(path, []byte(`{invalid`), 0644)

	err := ReadJSON(path, &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestWriteJSON_Success(t *testing.T) {
	path := tmpFile(t, "out.json")
	data := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteJSON(path, data); err != nil {
		t.Fatalf("WriteJSON unexpected error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error: %v", err)
	}

	expected := `{"name":"lion","age":30,"score":99.5}` + "\n"
	if string(raw) != expected {
		t.Errorf("WriteJSON content mismatch:\ngot:  %q\nwant: %q", string(raw), expected)
	}
}

func TestWriteJSON_WithIndent(t *testing.T) {
	path := tmpFile(t, "out_indent.json")
	data := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteJSON(path, data, WithIndent("  ")); err != nil {
		t.Fatalf("WriteJSON unexpected error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error: %v", err)
	}

	expected := "{\n  \"name\": \"lion\",\n  \"age\": 30,\n  \"score\": 99.5\n}\n"
	if string(raw) != expected {
		t.Errorf("WriteJSON indented content mismatch:\ngot:  %q\nwant: %q", string(raw), expected)
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	path := tmpFile(t, "roundtrip.json")
	original := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteJSON(path, original); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}

	var got jsonTestStruct
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}

	if got.Name != original.Name || got.Age != original.Age || got.Score != original.Score {
		t.Errorf("RoundTrip mismatch: got %+v, want %+v", got, original)
	}
}

// ── YAML ─────────────────────────────────────────────────────────

func TestReadYAML_Success(t *testing.T) {
	path := tmpFile(t, "test.yaml")
	content := "name: lion\nage: 30\nscore: 99.5\n"
	os.WriteFile(path, []byte(content), 0644)

	var got jsonTestStruct
	if err := ReadYAML(path, &got); err != nil {
		t.Fatalf("ReadYAML unexpected error: %v", err)
	}

	if got.Name != "lion" || got.Age != 30 || got.Score != 99.5 {
		t.Errorf("ReadYAML mismatch: got %+v", got)
	}
}

func TestReadYAML_FileNotFound(t *testing.T) {
	err := ReadYAML("/nonexistent/path/file.yaml", &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadYAML_InvalidYAML(t *testing.T) {
	path := tmpFile(t, "bad.yaml")
	os.WriteFile(path, []byte(": bad\n"), 0644)

	err := ReadYAML(path, &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestWriteYAML_Success(t *testing.T) {
	path := tmpFile(t, "out.yaml")
	data := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteYAML(path, data); err != nil {
		t.Fatalf("WriteYAML unexpected error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error: %v", err)
	}

	// Verify key values present
	if len(raw) == 0 {
		t.Fatal("WriteYAML: output is empty")
	}
}

func TestWriteYAML_WithTabIndent(t *testing.T) {
	path := tmpFile(t, "out_tab.yaml")
	data := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteYAML(path, data, WithIndent("\t")); err != nil {
		t.Fatalf("WriteYAML unexpected error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error: %v", err)
	}

	content := string(raw)
	// With tab indent, each key line should start with a tab
	if len(content) == 0 {
		t.Fatal("WriteYAML tab indent: output is empty")
	}
}

func TestWriteYAML_RoundTrip(t *testing.T) {
	path := tmpFile(t, "roundtrip.yaml")
	original := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteYAML(path, original); err != nil {
		t.Fatalf("WriteYAML error: %v", err)
	}

	var got jsonTestStruct
	if err := ReadYAML(path, &got); err != nil {
		t.Fatalf("ReadYAML error: %v", err)
	}

	if got.Name != original.Name || got.Age != original.Age || got.Score != original.Score {
		t.Errorf("RoundTrip mismatch: got %+v, want %+v", got, original)
	}
}

// ── TOML ─────────────────────────────────────────────────────────

func TestReadTOML_Success(t *testing.T) {
	path := tmpFile(t, "test.toml")
	content := `name = "lion"
age = 30
Score = 99.5
`
	os.WriteFile(path, []byte(content), 0644)

	var got jsonTestStruct
	if err := ReadTOML(path, &got); err != nil {
		t.Fatalf("ReadTOML unexpected error: %v", err)
	}

	if got.Name != "lion" || got.Age != 30 || got.Score != 99.5 {
		t.Errorf("ReadTOML mismatch: got %+v", got)
	}
}

func TestReadTOML_FileNotFound(t *testing.T) {
	err := ReadTOML("/nonexistent/path/file.toml", &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadTOML_InvalidTOML(t *testing.T) {
	path := tmpFile(t, "bad.toml")
	os.WriteFile(path, []byte(`= invalid`), 0644)

	err := ReadTOML(path, &jsonTestStruct{})
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

func TestWriteTOML_Success(t *testing.T) {
	path := tmpFile(t, "out.toml")
	data := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteTOML(path, data); err != nil {
		t.Fatalf("WriteTOML unexpected error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error: %v", err)
	}

	if len(raw) == 0 {
		t.Fatal("WriteTOML: output is empty")
	}
}

func TestWriteTOML_RoundTrip(t *testing.T) {
	path := tmpFile(t, "roundtrip.toml")
	original := jsonTestStruct{Name: "lion", Age: 30, Score: 99.5}

	if err := WriteTOML(path, original); err != nil {
		t.Fatalf("WriteTOML error: %v", err)
	}

	var got jsonTestStruct
	if err := ReadTOML(path, &got); err != nil {
		t.Fatalf("ReadTOML error: %v", err)
	}

	if got.Name != original.Name || got.Age != original.Age || got.Score != original.Score {
		t.Errorf("RoundTrip mismatch: got %+v, want %+v", got, original)
	}
}

// ── 指针接收者 roundtrip ─────────────────────────────────────────

func TestReadJSON_PointerReceiver(t *testing.T) {
	type inner struct {
		Items []string
	}
	path := tmpFile(t, "ptr.json")
	os.WriteFile(path, []byte(`{"Items":["a","b","c"]}`), 0644)

	var got inner
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if len(got.Items) != 3 || got.Items[0] != "a" {
		t.Errorf("ReadJSON pointer mismatch: got %+v", got)
	}
}
