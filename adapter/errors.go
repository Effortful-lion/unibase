package adapter

import "errors"

// AdapterError 适配器领域错误。
type AdapterError struct {
	code    string
	message string
}

// Error 返回错误描述。
func (e *AdapterError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}

// Code 返回错误码。
func (e *AdapterError) Code() string {
	return e.code
}

// 预定义错误。
var (
	ErrConfigRequired   = &AdapterError{code: "config_required", message: "config is required"}
	ErrRedisInitFailed  = &AdapterError{code: "redis_init_failed", message: "redis initialization failed"}
	ErrMySQLInitFailed  = &AdapterError{code: "mysql_init_failed", message: "mysql initialization failed"}
	ErrESInitFailed     = &AdapterError{code: "es_init_failed", message: "elasticsearch initialization failed"}
	ErrKafkaInitFailed  = &AdapterError{code: "kafka_init_failed", message: "kafka initialization failed"}
	ErrMongoInitFailed  = &AdapterError{code: "mongo_init_failed", message: "mongodb initialization failed"}
	ErrPrometheusFailed = &AdapterError{code: "prometheus_init_failed", message: "prometheus initialization failed"}
	ErrAlipayNotInit    = &AdapterError{code: "alipay_not_init", message: "alipay adapter not initialized"}
	ErrMinIONotInit     = &AdapterError{code: "minio_not_init", message: "minio adapter not initialized"}
)

// IsAdapterError 判断是否为 AdapterError。
func IsAdapterError(err error) bool {
	var adapterErr *AdapterError
	return errors.As(err, &adapterErr)
}
