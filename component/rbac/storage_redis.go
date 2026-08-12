package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultRedisTTL 是权限数据的默认过期时间（7 天）。
	defaultRedisTTL = 7 * 24 * time.Hour
)

// RedisStorage 基于 Redis 的存储实现，提供角色和权限的持久化存储，支持 TTL 自动过期和多租户隔离。
type RedisStorage struct {
	client redisCmd
	prefix string
	ttl    time.Duration
}

// Ensure RedisStorage implements Storage and StorageBulk.
var _ Storage = (*RedisStorage)(nil)
var _ StorageBulk = (*RedisStorage)(nil)

// redisIntCmd 封装 go-redis IntCmd 结果。
type redisIntCmd struct {
	cmd *redis.IntCmd
}

func (c *redisIntCmd) Err() error { return c.cmd.Err() }
func (c *redisIntCmd) Val() int64 { return c.cmd.Val() }

// redisBoolCmd 封装 go-redis BoolCmd 结果。
type redisBoolCmd struct {
	cmd *redis.BoolCmd
}

func (c *redisBoolCmd) Err() error { return c.cmd.Err() }
func (c *redisBoolCmd) Val() bool  { return c.cmd.Val() }

// redisPipeliner 封装 go-redis Pipeliner。
type redisPipeliner struct {
	pipe redis.Pipeliner
}

func (p *redisPipeliner) SAdd(ctx context.Context, key string, members ...interface{}) *redisIntCmd {
	return &redisIntCmd{cmd: p.pipe.SAdd(ctx, key, members...)}
}
func (p *redisPipeliner) Expire(ctx context.Context, key string, expiration time.Duration) *redisBoolCmd {
	return &redisBoolCmd{cmd: p.pipe.Expire(ctx, key, expiration)}
}
func (p *redisPipeliner) Exec(ctx context.Context) ([]interface{}, error) {
	cmders, err := p.pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(cmders))
	for i, c := range cmders {
		result[i] = c
	}
	return result, nil
}

// redisCmd 定义了存储所需的最小 Redis 操作接口。
type redisCmd interface {
	SAdd(ctx context.Context, key string, members ...string) *redisIntCmd
	SRem(ctx context.Context, key string, members ...string) *redisIntCmd
	SMembers(ctx context.Context, key string) ([]string, error)
	Expire(ctx context.Context, key string, expiration time.Duration) *redisBoolCmd
	Del(ctx context.Context, keys ...string) *redisIntCmd
	Pipeline() *redisPipeliner
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
}

// redisCmdAdapter 将 go-redis/v9 客户端适配为本框架的 redisCmd 接口。
type redisCmdAdapter struct {
	client interface {
		SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd
		SRem(ctx context.Context, key string, members ...interface{}) *redis.IntCmd
		SMembers(ctx context.Context, key string) ([]string, error)
		Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
		Del(ctx context.Context, keys ...interface{}) *redis.IntCmd
		Pipeline() redis.Pipeliner
		Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
	}
}

func (a *redisCmdAdapter) SAdd(ctx context.Context, key string, members ...string) *redisIntCmd {
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	return &redisIntCmd{cmd: a.client.SAdd(ctx, key, args...)}
}
func (a *redisCmdAdapter) SRem(ctx context.Context, key string, members ...string) *redisIntCmd {
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	return &redisIntCmd{cmd: a.client.SRem(ctx, key, args...)}
}
func (a *redisCmdAdapter) SMembers(ctx context.Context, key string) ([]string, error) {
	return a.client.SMembers(ctx, key)
}
func (a *redisCmdAdapter) Expire(ctx context.Context, key string, expiration time.Duration) *redisBoolCmd {
	return &redisBoolCmd{cmd: a.client.Expire(ctx, key, expiration)}
}
func (a *redisCmdAdapter) Del(ctx context.Context, keys ...string) *redisIntCmd {
	args := make([]interface{}, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	return &redisIntCmd{cmd: a.client.Del(ctx, args...)}
}
func (a *redisCmdAdapter) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	return a.client.Scan(ctx, cursor, match, count)
}
func (a *redisCmdAdapter) Pipeline() *redisPipeliner {
	return &redisPipeliner{pipe: a.client.Pipeline()}
}

// RedisStorageOption 配置 RedisStorage 的行为。
type RedisStorageOption func(*redisStorageOptions)

type redisStorageOptions struct {
	prefix string
	ttl    time.Duration
}

// WithRedisPrefix 设置 Redis key 前缀，用于多应用隔离。
// 默认前缀为 "auth"。
func WithRedisPrefix(prefix string) RedisStorageOption {
	return func(o *redisStorageOptions) {
		if prefix != "" {
			o.prefix = prefix
		}
	}
}

// WithRedisTTL 设置权限数据的默认过期时间。
// 传 0 表示永不过期。默认 7 天。
func WithRedisTTL(ttl time.Duration) RedisStorageOption {
	return func(o *redisStorageOptions) {
		o.ttl = ttl
	}
}

// NewRedisStorage 创建 Redis 存储实例。
//
// client 可以是实现 redisCmd 接口的任何 Redis 客户端。
// 使用 go-redis/v9 时，使用 RedisClientAdapter 包装：
//
//	import "github.com/redis/go-redis/v9"
//	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	storage := NewRedisStorage(RedisClientAdapter(client))
//
// 注意：调用方需要自行确保 github.com/redis/go-redis/v9 在 go.mod 中。
func NewRedisStorage(client redisCmd, opts ...RedisStorageOption) *RedisStorage {
	cfg := redisStorageOptions{
		prefix: "auth",
		ttl:    defaultRedisTTL,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &RedisStorage{
		client: client,
		prefix: cfg.prefix,
		ttl:    cfg.ttl,
	}
}

// ---------- Key 生成 ----------

// keySubjectRoles 生成主体角色映射 key：{prefix}:subject_roles:{subjectID}
func (r *RedisStorage) keySubjectRoles(subjectID string) string {
	return r.prefix + ":subject_roles:" + subjectID
}

// keyRolePerms 生成角色权限映射 key：{prefix}:role_perms:{roleName}
func (r *RedisStorage) keyRolePerms(roleName string) string {
	return r.prefix + ":role_perms:" + roleName
}

// keySubjectRolesDomain 生成多租户主体角色映射 key：{prefix}:subject_roles:{domain}::{subjectID}
func (r *RedisStorage) keySubjectRolesDomain(domain, subjectID string) string {
	return r.prefix + ":subject_roles:" + domain + "::" + subjectID
}

// keyRolePermsDomain 生成多租户角色权限映射 key：{prefix}:role_perms:{domain}::{roleName}
func (r *RedisStorage) keyRolePermsDomain(domain, roleName string) string {
	return r.prefix + ":role_perms:" + domain + "::" + roleName
}

// ---------- Subject ↔ Role（全局）----------

func (r *RedisStorage) AddRoleForSubject(ctx context.Context, subjectID, roleName string) error {
	key := r.keySubjectRoles(subjectID)
	if err := r.client.SAdd(ctx, key, roleName).Err(); err != nil {
		return err
	}
	if r.ttl > 0 {
		return r.client.Expire(ctx, key, r.ttl).Err()
	}
	return nil
}

func (r *RedisStorage) RemoveRoleForSubject(ctx context.Context, subjectID, roleName string) error {
	key := r.keySubjectRoles(subjectID)
	return r.client.SRem(ctx, key, roleName).Err()
}

func (r *RedisStorage) GetRolesForSubject(ctx context.Context, subjectID string) ([]string, error) {
	key := r.keySubjectRoles(subjectID)
	return r.client.SMembers(ctx, key)
}

// ---------- Subject ↔ Role（多租户）----------

func (r *RedisStorage) AddRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error {
	key := r.keySubjectRolesDomain(domain, subjectID)
	if err := r.client.SAdd(ctx, key, roleName).Err(); err != nil {
		return err
	}
	if r.ttl > 0 {
		return r.client.Expire(ctx, key, r.ttl).Err()
	}
	return nil
}

func (r *RedisStorage) RemoveRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error {
	key := r.keySubjectRolesDomain(domain, subjectID)
	return r.client.SRem(ctx, key, roleName).Err()
}

func (r *RedisStorage) GetRolesForSubjectInDomain(ctx context.Context, subjectID, domain string) ([]string, error) {
	key := r.keySubjectRolesDomain(domain, subjectID)
	return r.client.SMembers(ctx, key)
}

// ListDomainsForSubject 查询主体拥有的所有租户域（去重）。
func (r *RedisStorage) ListDomainsForSubject(ctx context.Context, subjectID string) ([]string, error) {
	prefix := r.prefix + ":subject_roles:"
	pattern := prefix + "*::" + subjectID
	keys, err := r.scanKeys(ctx, pattern)
	if err != nil {
		return nil, err
	}

	domainSet := make(map[string]bool)
	for _, key := range keys {
		domain, _ := r.extractSuffix(key, prefix)
		if domain != "" {
			domainSet[domain] = true
		}
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	return domains, nil
}

// ---------- Role ↔ Permission（全局）----------

func (r *RedisStorage) AddPermission(ctx context.Context, roleName, resource, action string) error {
	key := r.keyRolePerms(roleName)
	perm := resource + ":" + action
	if err := r.client.SAdd(ctx, key, perm).Err(); err != nil {
		return err
	}
	if r.ttl > 0 {
		return r.client.Expire(ctx, key, r.ttl).Err()
	}
	return nil
}

func (r *RedisStorage) RemovePermission(ctx context.Context, roleName, resource, action string) error {
	key := r.keyRolePerms(roleName)
	return r.client.SRem(ctx, key, resource+":"+action).Err()
}

func (r *RedisStorage) GetPermissions(ctx context.Context, roleName string) ([]Permission, error) {
	key := r.keyRolePerms(roleName)
	members, err := r.client.SMembers(ctx, key)
	if err != nil {
		return nil, err
	}
	return ParsePermissions(members)
}

// ---------- Role ↔ Permission（多租户）----------

func (r *RedisStorage) AddPermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error {
	key := r.keyRolePermsDomain(domain, roleName)
	perm := resource + ":" + action
	if err := r.client.SAdd(ctx, key, perm).Err(); err != nil {
		return err
	}
	if r.ttl > 0 {
		return r.client.Expire(ctx, key, r.ttl).Err()
	}
	return nil
}

func (r *RedisStorage) RemovePermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error {
	key := r.keyRolePermsDomain(domain, roleName)
	return r.client.SRem(ctx, key, resource+":"+action).Err()
}

func (r *RedisStorage) GetPermissionsInDomain(ctx context.Context, roleName, domain string) ([]Permission, error) {
	key := r.keyRolePermsDomain(domain, roleName)
	members, err := r.client.SMembers(ctx, key)
	if err != nil {
		return nil, err
	}
	return ParsePermissions(members)
}

// ---------- 批量操作 ----------

// LoadAll 通过 SCAN 遍历所有主体角色和角色权限 key。
func (r *RedisStorage) LoadAll(ctx context.Context) (map[string][]string, map[string][]Permission, error) {
	roleMembers := make(map[string][]string)
	rolePerms := make(map[string][]Permission)

	subjectPrefix := r.prefix + ":subject_roles:"
	rolePrefix := r.prefix + ":role_perms:"

	subjectKeys, err := r.scanKeys(ctx, subjectPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("scan subject_roles keys: %w", err)
	}
	for _, key := range subjectKeys {
		_, subjectID := r.extractSuffix(key, subjectPrefix)
		roles, err := r.client.SMembers(ctx, key)
		if err != nil {
			continue
		}
		if len(roles) > 0 {
			roleMembers[subjectID] = roles
		}
	}

	roleKeys, err := r.scanKeys(ctx, rolePrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("scan role_perms keys: %w", err)
	}
	for _, key := range roleKeys {
		domain, roleName := r.extractSuffix(key, rolePrefix)
		members, err := r.client.SMembers(ctx, key)
		if err != nil {
			continue
		}
		perms, _ := ParsePermissions(members)
		if len(perms) > 0 {
			if domain != "" {
				roleName = domain + ":" + roleName
			}
			rolePerms[roleName] = perms
		}
	}

	return roleMembers, rolePerms, nil
}

// SaveAll 先清空所有现有 key，再批量写入新数据。
func (r *RedisStorage) SaveAll(ctx context.Context, roleMembers map[string][]string, rolePerms map[string][]Permission) error {
	// 先清除旧数据，再写入新数据
	if err := r.clearAll(ctx); err != nil {
		return fmt.Errorf("clear old data: %w", err)
	}

	if err := r.writeAll(ctx, roleMembers, rolePerms); err != nil {
		return fmt.Errorf("write new data: %w", err)
	}

	return nil
}

// writeAll 将数据写入 Redis（不删除旧数据）。
func (r *RedisStorage) writeAll(ctx context.Context, roleMembers map[string][]string, rolePerms map[string][]Permission) error {
	pipe := r.client.Pipeline()

	for subjectID, roles := range roleMembers {
		key := r.keySubjectRoles(subjectID)
		for _, roleName := range roles {
			pipe.SAdd(ctx, key, roleName)
		}
		if r.ttl > 0 {
			pipe.Expire(ctx, key, r.ttl)
		}
	}

	for roleName, perms := range rolePerms {
		key := r.keyRolePerms(roleName)
		for _, perm := range perms {
			pipe.SAdd(ctx, key, perm.String())
		}
		if r.ttl > 0 {
			pipe.Expire(ctx, key, r.ttl)
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}

// clearAll 删除所有 auth 相关的 key。
func (r *RedisStorage) clearAll(ctx context.Context) error {
	pattern := r.prefix + ":*"
	keys, err := r.scanKeys(ctx, pattern)
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return r.client.Del(ctx, keys...).Err()
	}
	return nil
}

// scanKeys 使用 SCAN 遍历匹配 pattern 的 key。
func (r *RedisStorage) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64

	for {
		batch, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100)
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	return keys, nil
}

// extractSuffix 从 key 中提取前缀之后的部分。
// 对于带 domain 的 key（使用 :: 分隔），返回 (domain, suffix)。
// 对于不带 domain 的 key，返回 ("", suffix)。
func (r *RedisStorage) extractSuffix(key, prefix string) (string, string) {
	s := key[len(prefix):]
	// 检查是否包含 domain 分隔符 ::
	if idx := strings.Index(s, "::"); idx >= 0 {
		return s[:idx], s[idx+2:]
	}
	return "", s
}

// ---------- go-redis/v9 适配器 ----------

// RedisClientAdapter 将 go-redis/v9 客户端适配为本框架的 redisCmd 接口。
//
// 使用示例：
//
//	import "github.com/redis/go-redis/v9"
//	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	adapter := RedisClientAdapter(client)
//	storage := NewRedisStorage(adapter)
//
// 注意：调用方需要自行确保 github.com/redis/go-redis/v9 在 go.mod 中。
func RedisClientAdapter(client interface {
	SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd
	SRem(ctx context.Context, key string, members ...interface{}) *redis.IntCmd
	SMembers(ctx context.Context, key string) ([]string, error)
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...interface{}) *redis.IntCmd
	Pipeline() redis.Pipeliner
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
}) redisCmd {
	return &redisCmdAdapter{client: client}
}
