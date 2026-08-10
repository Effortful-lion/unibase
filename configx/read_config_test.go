package configx

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadConfig(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	cfg, err := ReadConfig(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%+v\n", cfg)
}
