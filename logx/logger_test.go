package logx

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.expected)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
		wantErr  bool
	}{
		{"debug", LevelDebug, false},
		{"INFO", LevelInfo, false},
		{"WARN", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"error", LevelError, false},
		{"FATAL", LevelFatal, false},
		{"unknown", LevelInfo, true},
	}

	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if err == nil && got != tt.expected {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestEntryFormat(t *testing.T) {
	entry := &Entry{
		Level:   LevelInfo,
		Module:  "user",
		File:    "service.go:42",
		Message: "用户登录成功",
		Fields:  Fields{"uid": 123, "ip": "10.0.0.1"},
	}
	formatted := entry.Format()
	if !strings.Contains(formatted, "INFO") {
		t.Error("missing level")
	}
	if !strings.Contains(formatted, "[user]") {
		t.Error("missing module")
	}
	if !strings.Contains(formatted, "service.go:42") {
		t.Error("missing file location")
	}
	if !strings.Contains(formatted, "用户登录成功") {
		t.Error("missing message")
	}
	if !strings.Contains(formatted, "uid=123") {
		t.Error("missing uid field")
	}
}

func TestConsoleWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelInfo)

	err := w.Write(&Entry{Level: LevelInfo, Message: "hello"})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", output)
	}
}

func TestConsoleWriterLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelWarn)

	_ = w.Write(&Entry{Level: LevelDebug, Message: "debug"})
	if buf.Len() > 0 {
		t.Error("debug message should be filtered")
	}

	_ = w.Write(&Entry{Level: LevelError, Message: "error"})
	if !strings.Contains(buf.String(), "error") {
		t.Error("error message should be written")
	}
}

func TestFileWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(w)

	_ = w.Write(&Entry{Level: LevelInfo, Message: "file log test"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if !strings.Contains(string(data), "file log test") {
		t.Errorf("expected log in file, got: %s", string(data))
	}
}

func TestFileWriterAutoCreateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "logs")
	path := filepath.Join(dir, "app.log")

	w, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileWriter should auto-create dir: %v", err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(w)
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Fatal(err)
		}
	}(dir)

	_ = w.Write(&Entry{Level: LevelInfo, Message: "auto dir test"})
}

func TestMultiWriter(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	w1 := NewConsoleWriter(&buf1, LevelInfo)
	w2 := NewConsoleWriter(&buf2, LevelDebug)
	mw := NewMultiWriter(w1, w2)

	_ = mw.Write(&Entry{Level: LevelInfo, Message: "shared"})

	if !strings.Contains(buf1.String(), "shared") {
		t.Error("buf1 should contain log")
	}
	if !strings.Contains(buf2.String(), "shared") {
		t.Error("buf2 should contain log")
	}
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelInfo)
	logger := New(w).With(Fields{"app": "myapp", "env": "test"})

	logger.Info("服务启动", Fields{"port": 8080})

	output := buf.String()
	if !strings.Contains(output, "app=myapp") {
		t.Error("missing fixed field 'app'")
	}
	if !strings.Contains(output, "env=test") {
		t.Error("missing fixed field 'env'")
	}
	if !strings.Contains(output, "port=8080") {
		t.Error("missing per-log field 'port'")
	}
}

func TestLoggerLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelWarn)
	logger := New(w)

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")

	output := buf.String()
	if strings.Contains(output, "debug") || strings.Contains(output, "info") {
		t.Error("debug/info should be filtered")
	}
	if !strings.Contains(output, "warn") {
		t.Error("warn should be output")
	}
}

func TestLoggerModuleWithInherit(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelInfo)
	logger := New(w).With(Fields{"service": "api"})

	userLog := logger.Module("user")
	userLog.Info("登录", Fields{"uid": 1})

	output := buf.String()
	if !strings.Contains(output, "[user]") {
		t.Error("missing module prefix")
	}
	if !strings.Contains(output, "service=api") {
		t.Error("missing inherited field")
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	var buf bytes.Buffer
	// 保存并替换 defaultLogger，避免污染其他测试
	orig := defaultLogger
	defaultLogger = New(NewConsoleWriter(&buf, LevelDebug))
	defer func() { defaultLogger = orig }()

	Info("包级别日志", Fields{"key": "val"})
	output := buf.String()
	if !strings.Contains(output, "包级别日志") {
		t.Error("missing message")
	}
	if !strings.Contains(output, "key=val") {
		t.Error("missing field")
	}
}

func TestFormatFunctions(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelInfo)
	logger := New(w)

	logger.Infof("用户 %d 登录, 来自 %s", 123, "10.0.0.1")
	output := buf.String()
	if !strings.Contains(output, "用户 123 登录, 来自 10.0.0.1") {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestCallerLocation(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelDebug)
	logger := New(w)

	logger.Debug("caller test")
	output := buf.String()
	if !strings.Contains(output, "logger_test.go") {
		t.Errorf("missing caller file, got: %s", output)
	}
}

func TestFieldsFormat_Sorted(t *testing.T) {
	f := Fields{"z": 1, "a": 2, "m": 3}
	got := f.format()
	want := "a=2 m=3 z=1"
	if got != want {
		t.Errorf("fields format: got %q, want %q", got, want)
	}
}

func TestConsoleWriter_WithJSONFormatter(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelInfo)
	w.SetFormatter(JSONFormatter)

	logger := New(w)
	logger.Info("hello", Fields{"uid": 123})

	output := buf.String()
	if !strings.Contains(output, `"message":"hello"`) {
		t.Errorf("expected json message, got: %s", output)
	}
	if !strings.Contains(output, `"uid":123`) {
		t.Errorf("expected json uid, got: %s", output)
	}
	if !strings.Contains(output, `"level":"INFO"`) {
		t.Errorf("expected json level, got: %s", output)
	}
}

func TestConsoleWriter_WithJSONFormatter_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelWarn)
	w.SetFormatter(JSONFormatter)

	_ = w.Write(&Entry{Level: LevelDebug, Message: "debug"})
	_ = w.Write(&Entry{Level: LevelError, Message: "error"})

	if strings.Contains(buf.String(), "debug") {
		t.Error("debug should be filtered")
	}
	if !strings.Contains(buf.String(), "error") {
		t.Error("error should be written")
	}
}

func TestFatalHook_Intercept(t *testing.T) {
	var buf bytes.Buffer
	hookCalled := false

	logger := New(NewConsoleWriter(&buf, LevelFatal)).SetFatalHook(func(e *Entry) bool {
		hookCalled = true
		return true // 拦截退出
	})

	logger.Fatal("should not exit")

	if !hookCalled {
		t.Error("fatal hook should be called")
	}
}

func TestDisableCaller(t *testing.T) {
	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, LevelDebug)

	// 开启 caller（默认）
	loggerWithCaller := New(w)
	loggerWithCaller.Debug("with caller")
	outputWithCaller := buf.String()
	if !strings.Contains(outputWithCaller, "logger_test.go") {
		t.Error("expected caller file in output")
	}

	buf.Reset()

	// 关闭 caller
	loggerNoCaller := New(w).DisableCaller()
	loggerNoCaller.Debug("no caller")
	outputNoCaller := buf.String()
	if strings.Contains(outputNoCaller, "logger_test.go") {
		t.Error("caller should be disabled")
	}
	if !strings.Contains(outputNoCaller, "no caller") {
		t.Error("message should still be present")
	}
}

func TestSetFatalHook_Chain(t *testing.T) {
	var buf bytes.Buffer
	hookCalled := false

	logger := New(NewConsoleWriter(&buf, LevelInfo)).
		With(Fields{"service": "api"}).
		SetFatalHook(func(e *Entry) bool {
			hookCalled = true
			if e.Fields["service"] != "api" {
				t.Error("expected inherited field 'service'")
			}
			return true
		})

	logger.Fatal("chained fatal")

	if !hookCalled {
		t.Error("fatal hook should be called")
	}
}

func TestFileWriter_SetFormatFn_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fw, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	// 设置 JSON 格式化
	fw.SetFormatter(JSONFormatter)

	logger := New(fw)
	logger.Info("hello", Fields{"uid": 123})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `"message":"hello"`) {
		t.Errorf("expected json message in file, got: %s", content)
	}
	if !strings.Contains(content, `"uid":123`) {
		t.Errorf("expected json uid in file, got: %s", content)
	}
}

func TestFileWriter_SetFormatFn_Nil_UsesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fw, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	// 不设置 formatter，使用默认文本格式
	logger := New(fw)
	logger.Info("hello")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[INFO]") {
		t.Errorf("expected text format in file, got: %s", content)
	}
}

func TestFileWriter_SetRotateSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fw, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	// 设置很小的轮转阈值，一条日志就触发
	fw.SetRotateSize(10)

	logger := New(fw)
	logger.Info("line 1")
	logger.Info("line 2")
	logger.Info("line 3")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 files after rotation, got %d", len(entries))
	}

	// 检查有序号文件
	hasSeq := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".001.") || strings.Contains(e.Name(), ".002.") {
			hasSeq = true
		}
	}
	if !hasSeq {
		t.Error("expected sequenced files like .001.log")
	}
}

func TestFileWriter_SetRotateSize_Disabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fw, err := NewFileWriter(path, LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer func(w *FileWriter) {
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	// 显式关闭轮转
	fw.SetRotateSize(0)

	logger := New(fw)
	logger.Info("line 1")
	logger.Info("line 2")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file without rotation, got %d", len(entries))
	}
}
