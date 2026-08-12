package adapter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// ESConfig Elasticsearch 连接配置。
type ESConfig struct {
	// Addresses ES 节点地址列表，必填。
	// 例如 []string{"http://localhost:9200"}
	Addresses []string

	// Username 基本认证用户名。
	Username string

	// Password 基本认证密码。
	Password string
}

// ES 是 Elasticsearch 客户端的薄封装。
// 持有标准库的 *elasticsearch.Client，核心能力直接委托给原始客户端。
type ES struct {
	client *elasticsearch.Client
}

// NewES 创建 Elasticsearch 适配器。
func NewES(cfg ESConfig) (*ES, error) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
	})
	if err != nil {
		return nil, err
	}

	return &ES{client: client}, nil
}

// Client 返回底层的 *elasticsearch.Client，可直接使用 ES SDK 的全部能力。
func (e *ES) Client() *elasticsearch.Client { return e.client }

// Ping 检查 ES 连接是否可用。
func (e *ES) Ping() error {
	_, err := e.client.Info()
	return err
}

// Transport 返回底层 Transport，可用于自定义配置。
func (e *ES) Transport() interface{} {
	return e.client.Transport
}

// Close Elasticsearch 客户端无需显式关闭（基于 HTTP 连接池自动管理）。
// 保留此方法以统一接口。
func (e *ES) Close() error {
	return nil
}

// ==================== 快捷操作 ====================

// Index 索引一个文档。doc 会被 JSON 序列化后发送。
// index 为索引名，id 为空时 ES 自动生成文档 ID。
func (e *ES) Index(ctx context.Context, index, id string, doc any) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index:      index,
		DocumentID: id,
		Body:       strings.NewReader(string(body)),
	}
	_, err = req.Do(ctx, e.client)
	return err
}

// Get 获取指定索引和 ID 的文档，结果反序列化到 dest。
// 文档不存在时返回 nil（非错误）。
func (e *ES) Get(ctx context.Context, index, id string, dest any) error {
	req := esapi.GetRequest{
		Index:      index,
		DocumentID: id,
	}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == 404 {
		return nil
	}

	return json.NewDecoder(res.Body).Decode(dest)
}

// Delete 删除指定索引和 ID 的文档。
func (e *ES) Delete(ctx context.Context, index, id string) error {
	req := esapi.DeleteRequest{
		Index:      index,
		DocumentID: id,
	}
	_, err := req.Do(ctx, e.client)
	return err
}

// Search 执行搜索，结果反序列化到 dest。
// query 为 ES Query DSL，如 `map[string]any{"match": map[string]any{"title": "hello"}}`。
func (e *ES) Search(ctx context.Context, index string, query map[string]any, dest any) error {
	body, err := json.Marshal(query)
	if err != nil {
		return err
	}

	req := esapi.SearchRequest{
		Index: []string{index},
		Body:  strings.NewReader(string(body)),
	}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	return json.NewDecoder(res.Body).Decode(dest)
}
