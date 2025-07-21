# GoFiber Session 数据读取工具

这个工具可以帮助您从 GoFiber 的 SQLite3 存储中读取所有 session 数据并转换为 JSON 数组。

## 功能特性

- ✅ 读取所有 session 数据
- ✅ 转换为结构化的 JSON 格式
- ✅ 显示过期状态和可读的过期时间
- ✅ 提供统计信息（总数、活跃数、过期数等）
- ✅ 支持导出到文件
- ✅ 提供 HTTP API 接口
- ✅ 支持命令行工具

## 快速开始

### 1. 集成到现有代码

将 `session_utils.go` 文件复制到您的项目中，然后在您的代码中调用：

```go
// 读取所有session数据
jsonData, err := ExportAllSessions(sys.Storage)
if err != nil {
    log.Printf("读取失败: %v", err)
    return
}

// 打印JSON数据
fmt.Println(string(jsonData))

// 保存到文件
err = SaveSessionsToFile(sys.Storage, "sessions_export.json")
if err != nil {
    log.Printf("保存失败: %v", err)
}

// 获取统计信息
stats, err := GetSessionStatistics(sys.Storage)
if err != nil {
    log.Printf("获取统计失败: %v", err)
} else {
    fmt.Printf("统计信息: %+v\n", stats)
}
```

### 2. 使用 HTTP API

参考 `example_integration.go` 文件，可以设置以下 API 端点：

```
GET  /api/v1/sessions/all     - 获取所有session数据
GET  /api/v1/sessions/stats   - 获取session统计信息  
POST /api/v1/sessions/export  - 导出session数据到文件
```

#### API 使用示例

```bash
# 获取所有session数据
curl http://localhost:3000/api/v1/sessions/all

# 获取统计信息
curl http://localhost:3000/api/v1/sessions/stats

# 导出到文件
curl -X POST "http://localhost:3000/api/v1/sessions/export?filename=my_sessions.json"
```

### 3. 命令行工具

```bash
# 直接打印所有session数据
go run . export-sessions
```

## 数据格式

### Session 记录格式

```json
{
  "key": "session_abc123",
  "value": "用户session数据",
  "value_hex": "e794a8e688b7session数据",
  "expires": 1703234567,
  "expires_at": "2023-12-22 10:30:00",
  "is_expired": false
}
```

字段说明：
- `key`: Session 的键值
- `value`: Session 的原始数据（字符串形式）
- `value_hex`: Session 数据的十六进制表示
- `expires`: Unix 时间戳格式的过期时间（0表示永不过期）
- `expires_at`: 可读格式的过期时间
- `is_expired`: 是否已过期

### 统计信息格式

```json
{
  "total": 10,
  "active": 8,
  "expired": 2,
  "never_expire": 3
}
```

字段说明：
- `total`: 总 session 数量
- `active`: 活跃（未过期）的 session 数量
- `expired`: 已过期的 session 数量
- `never_expire`: 永不过期的 session 数量

## 核心函数

### ExportAllSessions

```go
func ExportAllSessions(storage *sqlite3.Storage) ([]byte, error)
```

读取所有 session 数据并返回 JSON 字节数组。

### SaveSessionsToFile

```go
func SaveSessionsToFile(storage *sqlite3.Storage, filename string) error
```

将 session 数据保存到指定的文件中。

### GetSessionStatistics

```go
func GetSessionStatistics(storage *sqlite3.Storage) (map[string]interface{}, error)
```

获取 session 的统计信息。

## 系统要求

- Go 1.19+
- GoFiber v3
- SQLite3 支持

## 依赖包

```go
import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/storage/sqlite3/v2"
    "github.com/gofiber/fiber/v3/middleware/session"
)
```

## 注意事项

1. **数据库访问**: 确保您的应用程序对 SQLite 数据库文件有读取权限。

2. **并发安全**: 这些函数是并发安全的，可以在运行的应用程序中安全调用。

3. **内存使用**: 如果您有大量的 session 数据，`ExportAllSessions` 会将所有数据加载到内存中。

4. **过期检查**: 过期状态基于当前时间计算，不会自动清理过期的记录。

5. **数据格式**: Session 的 value 数据以字符串和十六进制两种格式提供，以便处理二进制数据。

## 故障排除

### 常见错误

1. **"查询session数据失败"**: 检查数据库文件是否存在且可访问。
2. **"转换JSON失败"**: 可能是 session 数据包含无法序列化的内容。
3. **"获取统计失败"**: 检查数据库连接和表结构。

### 调试技巧

1. 使用 `GetSessionStatistics` 先检查是否能正常连接数据库。
2. 检查您的 `sys.Storage` 配置是否正确。
3. 确认表名是否为 "sessions"（或您配置的表名）。

## 示例输出

```json
[
  {
    "key": "fiber_session_abc123",
    "value": "{\"user_id\":\"12345\",\"username\":\"testuser\"}",
    "value_hex": "7b2275736572...",
    "expires": 1703234567,
    "expires_at": "2023-12-22 10:30:00",
    "is_expired": false
  },
  {
    "key": "fiber_session_def456", 
    "value": "{\"user_id\":\"67890\",\"role\":\"admin\"}",
    "value_hex": "7b2275736572...",
    "expires": 0,
    "expires_at": "",
    "is_expired": false
  }
]
```

## 许可证

此代码基于您现有项目的许可证。