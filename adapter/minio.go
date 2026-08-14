package adapter

import (
	"context"
	"io"
	"time"

	"github.com/Effortful-lion/unibase/logx"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOConfig MinIO 对象存储配置。
type MinIOConfig struct {
	// Endpoint 存储服务地址，必填，例如 "s3.amazonaws.com" 或 "localhost:9000"。
	Endpoint string

	// AccessKey 访问密钥 ID，必填。
	AccessKey string

	// SecretKey 访问密钥，必填。
	SecretKey string

	// Bucket 默认存储桶名称，必填。
	Bucket string

	// UseSSL 是否使用 HTTPS，默认 false。
	UseSSL bool

	// Region 存储区域，可选。
	Region string
}

// MinIO 是 MinIO/S3 对象存储的薄封装。
// 持有标准库的 *minio.Client，核心能力直接委托给原始客户端。
type MinIO struct {
	client     *minio.Client
	bucket     string
	presignTTL time.Duration
	logger     *logx.Logger
}

// NewMinIO 创建 MinIO 适配器。
// 如果 bucket 已存在则直接使用，不存在则自动创建。
func NewMinIO(cfg MinIOConfig) (*MinIO, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err = client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{
			Region: cfg.Region,
		}); err != nil {
			return nil, err
		}
	}

	return &MinIO{
		client:     client,
		bucket:     cfg.Bucket,
		presignTTL: 7 * 24 * time.Hour,
		logger:     logx.Module("adapter.minio"),
	}, nil
}

// Client 返回底层的 *minio.Client，可直接使用 minio-go SDK 的全部能力。
func (m *MinIO) Client() *minio.Client { return m.client }

// Bucket 返回当前默认存储桶名称。
func (m *MinIO) Bucket() string { return m.bucket }

// ── 上传/下载 ─────────────────────────────────────────────────

// UploadObject 上传对象到默认存储桶。
// objectName 为对象键（路径），例如 "poster/abc.jpg"。
// contentType 为 MIME 类型，例如 "image/jpeg"，空字符串自动推断。
func (m *MinIO) UploadObject(ctx context.Context, objectName, contentType string, data []byte) (string, error) {
	if m == nil || m.client == nil {
		return "", ErrMinIONotInit
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := m.client.PutObject(ctx, m.bucket, objectName, nil, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		m.logger.Error("minio upload failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return "", err
	}

	return objectName, nil
}

// DownloadObject 从默认存储桶下载对象。
func (m *MinIO) DownloadObject(ctx context.Context, objectName string) ([]byte, error) {
	if m == nil || m.client == nil {
		return nil, ErrMinIONotInit
	}
	obj, err := m.client.GetObject(ctx, m.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		m.logger.Error("minio download failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return nil, err
	}
	defer obj.Close()

	return io.ReadAll(obj)
}

// DeleteObject 从默认存储桶删除对象。
func (m *MinIO) DeleteObject(ctx context.Context, objectName string) error {
	if m == nil || m.client == nil {
		return ErrMinIONotInit
	}
	if err := m.client.RemoveObject(ctx, m.bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
		m.logger.Error("minio delete failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return err
	}
	return nil
}

// ── 预签名 URL ────────────────────────────────────────────────

// PresignGet 生成对象下载预签名 URL。
// ttl 为 URL 有效期，0 表示使用默认 7 天。
func (m *MinIO) PresignGet(ctx context.Context, objectName string, ttl time.Duration) (string, error) {
	if m == nil || m.client == nil {
		return "", ErrMinIONotInit
	}
	if ttl == 0 {
		ttl = m.presignTTL
	}

	req, err := m.client.PresignedGetObject(ctx, m.bucket, objectName, ttl, nil)
	if err != nil {
		m.logger.Error("minio presign get failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return "", err
	}

	return req.String(), nil
}

// PresignPut 生成对象上传预签名 URL。
// ttl 为 URL 有效期，0 表示使用默认 7 天。
func (m *MinIO) PresignPut(ctx context.Context, objectName, contentType string, ttl time.Duration) (string, error) {
	if m == nil || m.client == nil {
		return "", ErrMinIONotInit
	}
	if ttl == 0 {
		ttl = m.presignTTL
	}

	req, err := m.client.PresignedPutObject(ctx, m.bucket, objectName, ttl)
	if err != nil {
		m.logger.Error("minio presign put failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return "", err
	}

	return req.String(), nil
}

// ── 工具方法 ──────────────────────────────────────────────────

// StatObject 获取对象元信息（大小、内容类型、最后修改时间）。
func (m *MinIO) StatObject(ctx context.Context, objectName string) (*minio.ObjectInfo, error) {
	if m == nil || m.client == nil {
		return nil, ErrMinIONotInit
	}
	info, err := m.client.StatObject(ctx, m.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		m.logger.Error("minio stat failed", logx.Fields{"error": err, "bucket": m.bucket, "object": objectName})
		return nil, err
	}
	return &info, nil
}

// ListObjects 列出默认存储桶中指定前缀的对象。
// prefix 例如 "poster/" 表示列出该目录下所有对象。
func (m *MinIO) ListObjects(ctx context.Context, prefix string) ([]minio.ObjectInfo, error) {
	if m == nil || m.client == nil {
		return nil, ErrMinIONotInit
	}
	var objects []minio.ObjectInfo
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix: prefix,
	}) {
		if obj.Err != nil {
			m.logger.Error("minio list failed", logx.Fields{"error": obj.Err, "bucket": m.bucket, "prefix": prefix})
			return nil, obj.Err
		}
		objects = append(objects, obj)
	}
	return objects, nil
}
