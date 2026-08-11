package id

import (
	"github.com/bwmarrin/snowflake"
)

// Snowflake 分布式 ID 生成器，基于 Twitter Snowflake 算法。
// 64bit 结构：1bit 保留 + 41bit 时间戳 + 10bit worker ID + 12bit 序列号。
// 底层使用开源库 github.com/bwmarrin/snowflake，本包仅做统一入口封装。
type Snowflake struct {
	node *snowflake.Node
}

// NewSnowflake 创建 Snowflake 生成器。
// workerID 必须唯一，范围 [0, 1023]。由调用方保证（如使用 zk/etcd 分配）。
func NewSnowflake(workerID int64) (*Snowflake, error) {
	node, err := snowflake.NewNode(workerID)
	if err != nil {
		return nil, err
	}
	return &Snowflake{node: node}, nil
}

// Generate 生成下一个分布式 ID，返回 int64。
func (s *Snowflake) Generate() int64 {
	return s.node.Generate().Int64()
}
