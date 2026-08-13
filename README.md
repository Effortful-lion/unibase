# unibase

unibase 是一个渐进式扩展的个人开发基础底座，面向后端服务开发，整合轻量基础工具、网络组件、高层服务封装；模块化划分，按需引入，避免依赖污染，持续迭代扩展。

# 模块

```
├── logx/       # 结构化日志
├── httpx/      # HTTP 客户端/服务端（Gin 增强）
├── rpcx/       # RPC 相关
├── adapter/    # 第三方服务客户端薄封装（redis、mysql、kafka、prometheus）
├── component/  # 通用组件（cache、gpool、poolx、state）
├── schedule/   # 定时任务触发（At / Every / After / Between / BetweenDaily）
├── plugins/    # 插件
├── tools/      # 工具函数（auth、breaker、crypto、encodingx、email、hash、id、limiter、random）
├── llmkit/     # LLM 开发库
├── configx/    # 配置管理
├── docs/       # 文档
```

各模块独立 README 见模块目录下。

# 演进方向

可能后期会做成：半自动化（个人风格固定的项目模板生成）+ 灵活扩展

# 当前不足

## tools/ 工具箱

| 工具 | 状态 | 不足 |
|------|------|------|
| `tools/crypto` | 基础可用 | 仅支持 AES-GCM，缺少 AES-CBC、ChaCha20-Poly1305、RSA 等场景 |
| `tools/random` | 基础可用 | 缺少可复现的伪随机（测试用）、Shuffle 等实用函数 |
| `tools/hash` | 基础可用 | 缺少 SHA-1、SHA-512、Hmac 系列 |
| `tools/id` | 基础可用 | Snowflake 使用开源库，缺少自定义 epoch 配置；无发号器（基于 DB/ZK 的递增 ID） |

## component/state 状态机

| 能力 | 状态 | 不足 |
|------|------|------|
| 内存存储 | 完整 | 无 |
| Redis 存储 | 简化版 | 无任务 CRUD、无 ZSet 调度索引、无原子状态转换（Lua 脚本）、无分布式锁 |
| 子任务 | 有基础支持 | 无子任务超时控制、无子任务失败重试策略 |
| 快照恢复 | 有基础支持 | 快照内容较薄，缺少执行进度深度恢复 |

## 通用不足

- **无字符串工具**：截断、分割、模板替换等高频操作缺失
- **无数据校验工具**：类似 `go-playground/validator` 的轻量封装缺失
- **无单元测试基础设施**：无测试 fixture、mock 生成、覆盖率报告模板
- **无 CI/CD 模板**：缺少 `.github/workflows` 或 Makefile 模板
- **文档不完整**：多数模块无独立 README，无 godoc 示例