# Triple协议连接池功能

## 概述

Triple协议的连接池功能已经集成到`clientManager`中，提供了HTTP连接的复用和管理能力。连接池可以显著提高性能，减少连接建立的开销。

## 功能特性

- **连接复用**: 复用HTTP/2和HTTP/3连接，减少连接建立开销
- **连接管理**: 自动管理连接的生命周期，包括创建、复用和清理
- **健康检查**: 定期检查连接健康状态，自动清理无效连接
- **配置灵活**: 支持通过URL参数配置连接池行为
- **统计信息**: 提供连接池使用情况的统计信息

## 配置参数

连接池支持以下配置参数，可以通过URL参数设置：

| 参数名 | 默认值 | 说明 |
|--------|--------|------|
| `max_connections` | 100 | 最大连接数 |
| `max_idle_connections` | 10 | 最大空闲连接数 |
| `idle_timeout` | 30s | 空闲连接超时时间 |
| `health_check_interval` | 10s | 健康检查间隔 |
| `max_retries` | 3 | 最大重试次数 |
| `retry_delay` | 100ms | 重试延迟时间 |

## 使用示例

### 1. 基本使用

```go
// 创建URL，配置连接池参数
url := common.NewURLWithOptions(
    common.WithProtocol("tri"),
    common.WithLocation("localhost:8080"),
    common.WithPath("com.example.UserService"),
    common.WithParamsValue("max_connections", "50"),
    common.WithParamsValue("max_idle_connections", "5"),
    common.WithParamsValue("idle_timeout", "30s"),
)

// 创建clientManager（自动启用连接池）
cm, err := newClientManager(url)
if err != nil {
    log.Fatal(err)
}
defer cm.close()

// 使用连接池进行调用
err = cm.callUnary(ctx, "getUser", req, resp)
if err != nil {
    log.Printf("调用失败: %v", err)
}
```

### 2. 获取连接池统计信息

```go
// 获取连接池统计信息
stats := cm.GetPoolStats()
fmt.Printf("总连接数: %d\n", stats["total_connections"])
fmt.Printf("活跃连接数: %d\n", stats["active_connections"])
fmt.Printf("空闲连接数: %d\n", stats["idle_connections"])
fmt.Printf("最大连接数: %d\n", stats["max_connections"])
```

### 3. 流式调用

```go
// 客户端流
stream, err := cm.callClientStream(ctx, "uploadData")
if err != nil {
    log.Printf("创建流失败: %v", err)
}
// 注意：流式调用的连接需要在流关闭时手动释放

// 服务端流
stream, err := cm.callServerStream(ctx, "downloadData", req)
if err != nil {
    log.Printf("创建流失败: %v", err)
}

// 双向流
stream, err := cm.callBidiStream(ctx, "chat")
if err != nil {
    log.Printf("创建流失败: %v", err)
}
```

## 工作原理

### 连接获取流程

1. 调用`getPooledConnection`方法
2. 检查连接池中是否存在可用的连接
3. 如果存在且健康，标记为活跃并返回
4. 如果不存在或不健康，创建新连接
5. 检查连接数限制，必要时清理无效连接

### 连接释放流程

1. 调用`releasePooledConnection`方法
2. 将连接标记为非活跃状态
3. 更新最后使用时间和使用计数
4. 连接回到空闲状态，等待下次复用

### 健康检查

- 定期检查连接的最后使用时间
- 超过`idle_timeout`的连接被标记为不健康
- 不健康的连接在下次清理时被移除

### 连接清理

- 定期清理超过`idle_timeout`的空闲连接
- 当连接数达到`max_connections`时，优先清理不健康的连接
- 保持空闲连接数不超过`max_idle_connections`

## 性能优化建议

1. **合理设置连接数**: 根据并发量和服务器能力设置`max_connections`
2. **调整超时时间**: 根据网络环境调整`idle_timeout`
3. **监控统计信息**: 定期检查连接池使用情况，及时调整配置
4. **避免频繁创建**: 尽量复用连接，减少连接建立开销

## 注意事项

1. **流式调用**: 流式调用的连接需要在流关闭时手动释放
2. **连接限制**: 注意设置合理的连接数限制，避免资源耗尽
3. **超时设置**: 合理设置空闲超时时间，平衡性能和资源使用
4. **错误处理**: 连接池会自动处理连接错误，但调用方仍需处理业务错误

## 兼容性

连接池功能向后兼容，现有的Triple客户端代码无需修改即可使用连接池功能。连接池的启用是自动的，可以通过URL参数进行配置。
