package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Effortful-lion/unibase/mux/internal/types"
)

// ── fakeDiscovery 用于测试 ClusterManager ─────────────────────

type fakeDiscovery struct {
	mu            sync.Mutex
	register      map[string]ClusterNode
	pullNodes     map[string][]ClusterNode
	registerErr   error
	unregisterErr error
}

func newFakeDiscovery() *fakeDiscovery {
	return &fakeDiscovery{
		register:  make(map[string]ClusterNode),
		pullNodes: make(map[string][]ClusterNode),
	}
}

func (f *fakeDiscovery) Register(_ context.Context, node ClusterNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.registerErr != nil {
		return f.registerErr
	}
	f.register[node.Tag] = node
	return nil
}

func (f *fakeDiscovery) Unregister(_ context.Context, node ClusterNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unregisterErr != nil {
		return f.unregisterErr
	}
	delete(f.register, node.Tag)
	return nil
}

func (f *fakeDiscovery) PullNodes(_ context.Context, group string, role Role, _ time.Duration) ([]ClusterNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := group + ":" + role.String()
	nodes, ok := f.pullNodes[key]
	if !ok {
		return nil, nil
	}
	return append([]ClusterNode(nil), nodes...), nil
}

func (f *fakeDiscovery) Watch(_ context.Context, group string, role Role, _ time.Duration) <-chan []ClusterNode {
	ch := make(chan []ClusterNode, 1)
	go func() {
		defer close(ch)
		nodes, _ := f.PullNodes(context.Background(), group, role, 0)
		ch <- nodes
	}()
	return ch
}

func (f *fakeDiscovery) setPullNodes(group string, role Role, nodes []ClusterNode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := group + ":" + role.String()
	f.pullNodes[key] = append([]ClusterNode(nil), nodes...)
}

// ── node.go 测试 ──────────────────────────────────────────────

func TestRole_String(t *testing.T) {
	tests := []struct {
		name string
		r    Role
		want string
	}{
		{"RoleMix", RoleMix, "mix"},
		{"RoleAP", RoleAP, "ap"},
		{"RoleBU", RoleBU, "bu"},
		{"Unknown", Role(255), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.String(); got != tt.want {
				t.Errorf("Role.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterNode_IsAlive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		node ClusterNode
		ttl  time.Duration
		want bool
	}{
		{"recent", ClusterNode{Ts: now.Unix()}, 10 * time.Second, true},
		{"expired", ClusterNode{Ts: now.Add(-20 * time.Second).Unix()}, 10 * time.Second, false},
		{"exact boundary", ClusterNode{Ts: now.Unix() - 1}, 1 * time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.node.IsAlive(tt.ttl); got != tt.want {
				t.Errorf("IsAlive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── manager.go 测试 ──────────────────────────────────────────

func TestClusterManager_StartStop(t *testing.T) {
	fd := newFakeDiscovery()
	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "test-1", ServiceName: "svc", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(100*time.Millisecond), WithClusterNodeTTL(1*time.Second))

	cm.Start()
	// 等待 goroutine 启动
	time.Sleep(50 * time.Millisecond)

	cm.Stop()

	// 确认 context 已取消
	select {
	case <-cm.ctx.Done():
		// ok
	default:
		t.Error("context should be cancelled after Stop")
	}
}

func TestClusterManager_GetNodes(t *testing.T) {
	fd := newFakeDiscovery()
	fd.setPullNodes("g1", RoleAP, []ClusterNode{
		{Tag: "ap-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
		{Tag: "ap-2", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
	})

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "mix-1", ServiceName: "svc", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(1*time.Minute), WithClusterNodeTTL(10*time.Minute))

	// 直接模拟 discoveryLoop 同步后的状态
	cm.nodesMu.Lock()
	cm.nodes.Store("ap-1", ClusterNode{Tag: "ap-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()})
	cm.nodes.Store("ap-2", ClusterNode{Tag: "ap-2", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()})
	cm.nodesMu.Unlock()

	nodes := cm.GetNodes("g1", RoleAP)
	if len(nodes) != 2 {
		t.Errorf("GetNodes() returned %d nodes, want 2", len(nodes))
	}
}

func TestClusterManager_GetNodes_FiltersByGroupAndRole(t *testing.T) {
	fd := newFakeDiscovery()
	fd.setPullNodes("g1", RoleAP, []ClusterNode{
		{Tag: "ap-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
		{Tag: "ap-2", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
	})

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", ServiceName: "svc", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(1*time.Minute), WithClusterNodeTTL(10*time.Minute))

	// 直接模拟同步后的状态
	cm.nodesMu.Lock()
	cm.nodes.Store("ap-1", ClusterNode{Tag: "ap-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()})
	cm.nodes.Store("ap-2", ClusterNode{Tag: "ap-2", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()})
	cm.nodesMu.Unlock()

	apNodes := cm.GetNodes("g1", RoleAP)
	if len(apNodes) != 2 {
		t.Errorf("GetNodes(RoleAP) returned %d nodes, want 2", len(apNodes))
	}

	// 不同 role 返回空
	mixNodes := cm.GetNodes("g1", RoleMix)
	if len(mixNodes) != 0 {
		t.Errorf("GetNodes(RoleMix) = %v, want []", mixNodes)
	}
}

func TestClusterManager_GetNodes_ExcludesSelf(t *testing.T) {
	fd := newFakeDiscovery()
	fd.setPullNodes("g1", RoleAP, []ClusterNode{
		{Tag: "self", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
		{Tag: "peer-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()},
	})

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", Group: "g1", Role: RoleAP, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(1*time.Minute), WithClusterNodeTTL(10*time.Minute))

	// 直接模拟 discoveryLoop 排除 self 后的状态
	cm.nodesMu.Lock()
	cm.nodes.Store("peer-1", ClusterNode{Tag: "peer-1", Group: "g1", Role: RoleAP, Ts: time.Now().Unix()})
	cm.nodesMu.Unlock()

	nodes := cm.GetNodes("g1", RoleAP)
	if len(nodes) != 1 || nodes[0].Tag != "peer-1" {
		t.Errorf("GetNodes() should exclude self, got %v", nodes)
	}
}

func TestClusterManager_GetNodes_ExpiredFiltered(t *testing.T) {
	fd := newFakeDiscovery()
	now := time.Now()
	fd.setPullNodes("g1", RoleAP, []ClusterNode{
		{Tag: "alive", Group: "g1", Role: RoleAP, Ts: now.Unix()},
		{Tag: "stale", Group: "g1", Role: RoleAP, Ts: now.Add(-2 * time.Minute).Unix()},
	})

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", Group: "g1", Role: RoleAP, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(1*time.Minute), WithClusterNodeTTL(1*time.Minute))

	cm.nodesMu.Lock()
	cm.nodes.Store("alive", ClusterNode{Tag: "alive", Group: "g1", Role: RoleAP, Ts: now.Unix()})
	cm.nodes.Store("stale", ClusterNode{Tag: "stale", Group: "g1", Role: RoleAP, Ts: now.Add(-2 * time.Minute).Unix()})
	cm.nodesMu.Unlock()

	nodes := cm.GetNodes("g1", RoleAP)
	if len(nodes) != 1 || nodes[0].Tag != "alive" {
		t.Errorf("GetNodes() should filter expired, got %v", nodes)
	}
}

func TestClusterManager_StartStop_GoroutineLeak(t *testing.T) {
	fd := newFakeDiscovery()
	fd.registerErr = context.DeadlineExceeded

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "test", ServiceName: "svc", Group: "g1", Role: RoleAP, IPPort: ":8080",
	}, fd, WithClusterHeartbeatInterval(50*time.Millisecond), WithClusterNodeTTL(10*time.Minute))

	cm.Start()

	// 等待心跳 goroutine 启动并至少执行一次
	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		cm.Stop()
	}()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s — goroutine leak")
	}

	select {
	case <-cm.ctx.Done():
		// ok
	default:
		t.Error("context should be cancelled after Stop")
	}
}

func TestClusterManager_Forward(t *testing.T) {
	// 创建一个本地 HTTP 服务器，验证转发内容
	var receivedMsg *types.Message
	var receivedCmd string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		var msg types.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		receivedMsg = &msg
		receivedCmd = msg.Cmd
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	fd := newFakeDiscovery()
	fd.setPullNodes("g1", RoleAP, []ClusterNode{
		{Tag: "target", Group: "g1", Role: RoleAP, ConnectUrl: ts.URL},
	})

	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd)

	msg := &types.Message{Cmd: "hello", Head: map[string]string{"k": "v"}, Body: []byte("world")}
	target := ClusterNode{Tag: "target", Group: "g1", Role: RoleAP, ConnectUrl: ts.URL}

	err := cm.Forward(context.Background(), &target, msg)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if receivedCmd != "hello" {
		t.Errorf("forwarded cmd = %s, want hello", receivedCmd)
	}
	if string(receivedMsg.Body) != "world" {
		t.Errorf("forwarded body = %s, want world", receivedMsg.Body)
	}
}

func TestClusterManager_Forward_Non2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer ts.Close()

	fd := newFakeDiscovery()
	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd)

	target := ClusterNode{Tag: "target", Group: "g1", Role: RoleAP, ConnectUrl: ts.URL}
	err := cm.Forward(context.Background(), &target, &types.Message{Cmd: "test"})
	if err == nil {
		t.Fatal("Forward() expected error on non-2xx, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Forward() error = %v, want contain 400", err)
	}
}

func TestClusterManager_Forward_CustomCmdPath(t *testing.T) {
	var receivedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	fd := newFakeDiscovery()
	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "self", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, fd, WithClusterCmdPath("/custom/cmd"))

	target := ClusterNode{Tag: "target", Group: "g1", Role: RoleAP, ConnectUrl: ts.URL}
	msg := &types.Message{Cmd: "hello", Body: []byte("world")}

	err := cm.Forward(context.Background(), &target, msg)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if receivedPath != "/custom/cmd" {
		t.Errorf("Forward() path = %s, want /custom/cmd", receivedPath)
	}
}

// ── forward.go 单元测试 ──────────────────────────────────────

func TestEncodeMessage(t *testing.T) {
	msg := &types.Message{
		Cmd:  "cmd1",
		Head: map[string]string{"a": "b"},
		Body: []byte("hello"),
	}
	b, err := encodeMessage(msg)
	if err != nil {
		t.Fatalf("encodeMessage() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["cmd"] != "cmd1" {
		t.Errorf("cmd = %v, want cmd1", decoded["cmd"])
	}
	// JSON 编码 []byte 为 base64
	if decoded["body"] != "aGVsbG8=" {
		t.Errorf("body = %v, want aGVsbG8=", decoded["body"])
	}
}

func TestClusterManager_Stop_CallsCloseFn(t *testing.T) {
	closed := false
	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "test", ServiceName: "svc", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, newFakeDiscovery())
	cm.SetCloseFn(func() error {
		closed = true
		return nil
	})

	cm.Stop()

	if !closed {
		t.Error("closeFn should be called during Stop")
	}
}

func TestClusterManager_Stop_CloseFnError(t *testing.T) {
	cm := NewClusterManager(context.Background(), ClusterNode{
		Tag: "test", ServiceName: "svc", Group: "g1", Role: RoleMix, IPPort: ":8080",
	}, newFakeDiscovery())
	cm.SetCloseFn(func() error {
		return fmt.Errorf("close failed")
	})

	// Stop 不应因 closeFn 错误而 panic
	cm.Stop()
}
