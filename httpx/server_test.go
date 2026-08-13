package httpx

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestServerOptions(t *testing.T) {
	// 验证选项函数能正确设置 config 字段
	var gotHookCalled bool

	cfg := defaultServerConfig()
	WithShutdownTimeout(5 * time.Second)(cfg)
	WithShutdownLogger(nil)(cfg)
	WithShutdownHook(func(ctx context.Context) error {
		gotHookCalled = true
		return nil
	})(cfg)

	if cfg.timeout != 5*time.Second {
		t.Errorf("timeout: got %v, want %v", cfg.timeout, 5*time.Second)
	}
	if cfg.preShutdown == nil {
		t.Error("preShutdown hook not set")
	}

	// 触发 hook
	cfg.preShutdown(nil)
	if !gotHookCalled {
		t.Error("preShutdown hook was not called")
	}
}

func TestServerOptions_Defaults(t *testing.T) {
	cfg := defaultServerConfig()

	if cfg.timeout != 30*time.Second {
		t.Errorf("default timeout: got %v, want %v", cfg.timeout, 30*time.Second)
	}
	if cfg.logger != nil {
		t.Errorf("default logger: got non-nil, want nil")
	}
	if cfg.preShutdown != nil {
		t.Errorf("default preShutdown: got non-nil, want nil")
	}
}

func TestRun_SimpleServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 在 goroutine 中启动服务
	go func() {
		if err := Run(r, ":9876"); err != nil {
			// Run 会返回 http.ErrServerClosed 或 listener error，正常
		}
	}()

	// 给服务一点时间启动
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:9876/healthz")
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("health check status: got %d, want %d", resp.StatusCode, 200)
	}
}
