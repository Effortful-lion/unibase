package delayqueue

import "github.com/redis/go-redis/v9"

// pollScript 使用 ZPOPMINBYSCORE 原子弹出到期消息并移入 Processing 队列。
// Redis 6.2+ 支持 ZPOPMINBYSCORE，保证原子性：读和删一步完成。
//
// ZPOPMINBYSCORE 返回值：仅弹出成员的字符串列表（无 score）。
//
// KEYS[1] = delay queue key
// KEYS[2] = processing queue key
// ARGV[1] = batch size (must be > 0)
// ARGV[2] = current timestamp (unix nano)
//
// 返回值：弹出并移入 Processing 的消息 JSON 列表。
var pollScript = redis.NewScript(`
local batch = tonumber(ARGV[1])
if batch <= 0 then
    return {}
end

local now = tonumber(ARGV[2])

local popped = redis.call('ZPOPMINBYSCORE', KEYS[1], '-inf', now, 'COUNT', batch)
for i = 1, #popped do
    redis.call('ZADD', KEYS[2], now, popped[i])
end

return popped
`)

// nackScript 原子处理 Nack：从 Processing 移除，按重试次数决定重试或 DLQ。
// 使用 lockdog 锁保护，防止 Nack 与 Sweep 并发修改同一条消息。
//
// KEYS[1] = processing queue key
// KEYS[2] = delay queue key (requeue)
// KEYS[3] = dlq key
// KEYS[4] = lock key
// ARGV[1] = lock token
// ARGV[2] = member JSON
// ARGV[3] = max retries
// ARGV[4] = requeue score (unix nano)
// ARGV[5] = new retry count
// ARGV[6] = new member JSON (for requeue)
// ARGV[7] = lock TTL (ms)
//
// 返回值：1 = requeued, 2 = dlq, 0 = lock not held (skip)
var nackScript = redis.NewScript(`
-- 尝试获取分布式锁
local lockAcquired = redis.call('SET', KEYS[4], ARGV[1], 'NX', 'PX', ARGV[7])
if not lockAcquired then
    return 0
end

-- 从 Processing 移除
redis.call('ZREM', KEYS[1], ARGV[2])

local retries = tonumber(ARGV[5])
local maxRetries = tonumber(ARGV[3])

if retries >= maxRetries then
    -- 移入 DLQ
    redis.call('RPUSH', KEYS[3], ARGV[2])
    -- 释放锁
    redis.call('DEL', KEYS[4])
    return 2
else
    -- 退避重试：重新入延迟队列
    redis.call('ZADD', KEYS[2], ARGV[4], ARGV[6])
    -- 释放锁
    redis.call('DEL', KEYS[4])
    return 1
end
`)

// dlqPopScript 原子地从 DLQ List 中弹出 N 条消息。
//
// KEYS[1] = dlq key
// ARGV[1] = count (must be > 0)
//
// 返回值：弹出的消息 JSON 列表。
var dlqPopScript = redis.NewScript(`
local count = tonumber(ARGV[1])
if count <= 0 then
    return {}
end
local results = {}
for i = 1, count do
    local val = redis.call('LPOP', KEYS[1])
    if not val then
        break
    end
    results[i] = val
end
return results
`)
