package adapter

import (
	"context"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/twmb/franz-go/pkg/kgo"
)

// KafkaConfig Kafka 连接配置。
type KafkaConfig struct {
	// Brokers Kafka 服务器地址列表，必填。
	// 例如 []string{"localhost:9092"}
	Brokers []string

	// ClientID 客户端标识。
	ClientID string
}

// Kafka 是 Kafka 客户端的薄封装。
// 持有标准库的 *kgo.Client，同时提供 Producer/Consume 快捷方法。
type Kafka struct {
	client *kgo.Client
	logger *logx.Logger
}

// NewKafka 创建 Kafka 适配器。
func NewKafka(cfg KafkaConfig) (*Kafka, error) {
	opt := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
	}
	if cfg.ClientID != "" {
		opt = append(opt, kgo.ClientID(cfg.ClientID))
	}

	client, err := kgo.NewClient(opt...)
	if err != nil {
		return nil, err
	}

	return &Kafka{
		client: client,
		logger: logx.Module("adapter.kafka"),
	}, nil
}

// Client 返回底层的 *kgo.Client，可直接使用 franz-go 的全部能力。
func (k *Kafka) Client() *kgo.Client { return k.client }

// Ping 检查 Kafka 连接是否可用。
func (k *Kafka) Ping(ctx context.Context) error {
	return k.client.Ping(ctx)
}

// Produce 同步发送单条消息。
func (k *Kafka) Produce(ctx context.Context, topic string, key, value []byte) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   key,
		Value: value,
	}
	results := k.client.ProduceSync(ctx, record)
	return results.FirstErr()
}

// Consume 消费指定 topic 的消息。
// f 回调函数接收每条消息，返回 false 时停止消费。
// 此方法为阻塞调用，直到 ctx 取消或 f 返回 false。
func (k *Kafka) Consume(ctx context.Context, topics []string, f func(*kgo.Record) bool) error {
	k.client.AddConsumeTopics(topics...)

	for {
		fetches := k.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 记录 fetch 级别的错误
		if err := fetches.Err(); err != nil {
			k.logger.Error("kafka fetch error", logx.Fields{"err": err.Error()})
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			if !f(record) {
				return nil
			}
		}
	}
}

// Close 关闭 Kafka 客户端。
func (k *Kafka) Close() error {
	k.client.Close()
	return nil
}
