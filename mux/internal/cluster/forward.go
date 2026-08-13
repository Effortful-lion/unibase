package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/types"
)

// Forwarder 负责将消息转发到目标节点。
type Forwarder struct {
	client  *http.Client
	logger  *logx.Logger
	cmdPath string
}

// NewForwarder 创建转发器。
func NewForwarder(logger *logx.Logger, cmdPath string) *Forwarder {
	if logger == nil {
		logger = logx.Default()
	}
	if cmdPath == "" {
		cmdPath = "/v1/cmd"
	}
	return &Forwarder{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:  logger,
		cmdPath: cmdPath,
	}
}

// Forward 将消息 POST 到目标节点的 Cmd 入口。
func (f *Forwarder) Forward(ctx context.Context, target *ClusterNode, msg *types.Message) error {
	url := target.ConnectUrl + f.cmdPath

	body, err := encodeMessage(msg)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		f.logger.Error("forward failed", logx.Fields{"error": err, "target": target.ConnectUrl})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("forward: target returned %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

func encodeMessage(msg *types.Message) ([]byte, error) {
	return json.Marshal(map[string]any{
		"cmd":  msg.Cmd,
		"head": msg.Head,
		"body": msg.Body,
	})
}
