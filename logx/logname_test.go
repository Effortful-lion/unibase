package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogNamePattern_ModuleDate(t *testing.T) {
	ln := NewLogNamePattern().Module().Char("_").Date("2006-01-02").Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	want := "pg_2026-08-01.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogNamePattern_DateModule(t *testing.T) {
	ln := NewLogNamePattern().Date("2006-01-02").Char("_").Module().Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	want := "2026-08-01_pg.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogNamePattern_WithClock(t *testing.T) {
	ln := NewLogNamePattern().
		Module().
		Char("_").
		Date("2006-01-02").
		Char("_").
		Clock("15-04-05").
		Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC)})
	want := "pg_2026-08-01_12-30-45.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogNamePattern_Timestamp(t *testing.T) {
	ln := NewLogNamePattern().Timestamp().Char("_").Module().Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	if !strings.HasPrefix(got, "17") || !strings.HasSuffix(got, "_pg.log") {
		t.Errorf("got %q, unexpected format", got)
	}
}

func TestLogNamePattern_OnlyDate(t *testing.T) {
	ln := NewLogNamePattern().Date("2006-01-02").Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	want := "2026-08-01.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogNamePattern_CustomPath(t *testing.T) {
	ln := NewLogNamePattern().Module().Char("/").Date("2006-01-02").Char("/app").Build()
	got := ln(LogMeta{Module: "pg", Time: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	want := "pg/2026-08-01/app.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewFileWriterWithLogName_ModuleDate(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")

	ln := NewLogNamePattern().Module().Char("_").Date("2006-01-02").Build()
	fw, err := NewFileWriterWithLogName(dir, LevelInfo, ln)
	if err != nil {
		t.Fatal(err)
	}
	defer func(fw *FileWriter) {
		err := fw.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	l := New(fw).Module("pg")
	l.Info("hello world")

	wantFile := filepath.Join(dir, "pg_"+today+".log")
	data, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("expected file %q: %v", wantFile, err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("log content not found in %q: %s", wantFile, string(data))
	}
}

func TestNewFileWriterWithLogName_SwitchFile(t *testing.T) {
	dir := t.TempDir()

	ln := NewLogNamePattern().Date("2006-01-02").Build()
	fw, err := NewFileWriterWithLogName(dir, LevelInfo, ln)
	if err != nil {
		t.Fatal(err)
	}
	defer func(fw *FileWriter) {
		err := fw.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	entryA := &Entry{
		Time:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Level:   LevelInfo,
		Module:  "pg",
		Message: "day1",
	}
	_ = fw.Write(entryA)

	entryB := &Entry{
		Time:    time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		Level:   LevelInfo,
		Module:  "pg",
		Message: "day2",
	}
	_ = fw.Write(entryB)

	for _, name := range []string{"2026-08-01.log", "2026-08-02.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			t.Errorf("expected file %q not found", name)
		}
	}
}

func TestNewFileWriterWithLogName_CustomPath(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")

	ln := NewLogNamePattern().Module().Char("/").Date("2006-01-02").Char("/app").Build()
	fw, err := NewFileWriterWithLogName(dir, LevelInfo, ln)
	if err != nil {
		t.Fatal(err)
	}
	defer func(fw *FileWriter) {
		err := fw.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(fw)

	l := New(fw).Module("pg")
	l.Info("custom path")

	wantFile := filepath.Join(dir, "pg", today, "app.log")
	if _, err := os.Stat(wantFile); os.IsNotExist(err) {
		t.Errorf("expected file %q not found", wantFile)
	}
}
