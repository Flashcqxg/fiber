package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/storage/sqlite3/v2"
	"github.com/gofiber/fiber/v3/middleware/session"
)

// 这个文件展示了如何在您的现有代码中集成session数据读取功能

// 假设这是您的系统结构
type System struct {
	Storage *sqlite3.Storage
	US      *session.Store
}

// 初始化系统（这部分类似您现有的代码）
func InitializeSystem() *System {
	sys := &System{}
	
	// 创建SQLite3存储
	sys.Storage = sqlite3.New(sqlite3.Config{
		Database: "GEServer.db?_journal_mode=WAL",
		Table:    "sessions",
	})
	
	// 创建session存储
	sys.US = session.NewStore(session.Config{
		Storage: sys.Storage,
	})
	
	return sys
}

// API处理函数：获取所有session数据
func (sys *System) GetAllSessionsAPI(c fiber.Ctx) error {
	// 使用我们创建的函数读取所有session数据
	jsonData, err := ExportAllSessions(sys.Storage)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "读取session数据失败",
			"details": err.Error(),
		})
	}

	// 将JSON字符串解析为interface{}以便返回
	var sessions interface{}
	if err := json.Unmarshal(jsonData, &sessions); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "解析JSON失败",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": sessions,
	})
}

// API处理函数：获取session统计信息
func (sys *System) GetSessionStatsAPI(c fiber.Ctx) error {
	stats, err := GetSessionStatistics(sys.Storage)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "获取统计信息失败",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"stats": stats,
	})
}

// API处理函数：导出session数据到文件
func (sys *System) ExportSessionsAPI(c fiber.Ctx) error {
	filename := c.Query("filename", "sessions_export.json")
	
	err := SaveSessionsToFile(sys.Storage, filename)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "导出文件失败",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("Session数据已导出到 %s", filename),
		"filename": filename,
	})
}

// 设置路由
func (sys *System) SetupRoutes(app *fiber.App) {
	// API路由组
	api := app.Group("/api/v1")
	
	// Session相关路由
	sessions := api.Group("/sessions")
	sessions.Get("/all", sys.GetAllSessionsAPI)        // 获取所有session数据
	sessions.Get("/stats", sys.GetSessionStatsAPI)     // 获取session统计
	sessions.Post("/export", sys.ExportSessionsAPI)    // 导出session数据
}

// 命令行工具：直接打印所有session数据
func (sys *System) PrintAllSessions() {
	fmt.Println("=== 读取所有Session数据 ===")
	
	jsonData, err := ExportAllSessions(sys.Storage)
	if err != nil {
		log.Printf("读取失败: %v", err)
		return
	}
	
	fmt.Println(string(jsonData))
	
	// 获取统计信息
	fmt.Println("\n=== Session统计信息 ===")
	stats, err := GetSessionStatistics(sys.Storage)
	if err != nil {
		log.Printf("获取统计失败: %v", err)
		return
	}
	
	statsJSON, _ := json.MarshalIndent(stats, "", "  ")
	fmt.Println(string(statsJSON))
}

// 主函数示例
func main() {
	// 初始化系统
	sys := InitializeSystem()
	defer sys.Storage.Close()

	// 创建Fiber应用
	app := fiber.New(fiber.Config{
		AppName: "GE Server",
	})

	// 设置路由
	sys.SetupRoutes(app)

	// 如果是命令行模式，直接打印session数据
	if len(os.Args) > 1 && os.Args[1] == "export-sessions" {
		sys.PrintAllSessions()
		return
	}

	// 启动服务器
	log.Fatal(app.Listen(":3000"))
}

/* 
使用方法：

1. API方式：
   启动服务器后，可以通过以下API访问：
   - GET /api/v1/sessions/all    - 获取所有session数据
   - GET /api/v1/sessions/stats  - 获取session统计
   - POST /api/v1/sessions/export?filename=my_sessions.json - 导出到文件

2. 命令行方式：
   go run . export-sessions

3. 编程方式：
   在您的代码中直接调用：
   jsonData, err := ExportAllSessions(sys.Storage)

示例API响应：

GET /api/v1/sessions/all:
{
  "success": true,
  "data": [
    {
      "key": "session_123",
      "value": "user_data_here",
      "value_hex": "757365725f646174615f68657265",
      "expires": 1703234567,
      "expires_at": "2023-12-22 10:30:00",
      "is_expired": false
    }
  ]
}

GET /api/v1/sessions/stats:
{
  "success": true,
  "stats": {
    "total": 10,
    "active": 8,
    "expired": 2,
    "never_expire": 3
  }
}
*/