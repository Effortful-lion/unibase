/*
Package cache 提供多级缓存能力。

当前包含：

  - local_lru.go   本地 LRU 缓存（带 TTL、淘汰回调、命中率统计）
  - remote_redis.go 远程 Redis 缓存（单飞防击穿、空值缓存、本地回退）
*/
package cache
