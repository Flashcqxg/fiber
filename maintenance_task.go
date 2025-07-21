package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite 驱动
)

// SQLiteMaintenanceManager SQLite 维护管理器
type SQLiteMaintenanceManager struct {
	dbPath   string
	stopChan chan struct{}
}

// NewSQLiteMaintenanceManager 创建新的维护管理器
func NewSQLiteMaintenanceManager(dbPath string) *SQLiteMaintenanceManager {
	return &SQLiteMaintenanceManager{
		dbPath:   dbPath,
		stopChan: make(chan struct{}),
	}
}

// Start 启动定期维护任务
func (m *SQLiteMaintenanceManager) Start() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 每5分钟执行一次
		defer ticker.Stop()
		
		fmt.Println("SQLite 维护任务已启动，每5分钟执行一次")
		
		for {
			select {
			case <-ticker.C:
				m.performMaintenance()
			case <-m.stopChan:
				fmt.Println("SQLite 维护任务已停止")
				return
			}
		}
	}()
}

// Stop 停止维护任务
func (m *SQLiteMaintenanceManager) Stop() {
	close(m.stopChan)
}

// performMaintenance 执行维护任务
func (m *SQLiteMaintenanceManager) performMaintenance() {
	db, err := sql.Open("sqlite3", m.dbPath+"?_journal_mode=WAL&_busy_timeout=10000")
	if err != nil {
		fmt.Printf("维护任务连接数据库失败: %v\n", err)
		return
	}
	defer db.Close()
	
	// 1. 检查 WAL 文件大小
	var walPages int
	err = db.QueryRow("PRAGMA wal_checkpoint").Scan(&walPages)
	if err != nil {
		fmt.Printf("检查 WAL 状态失败: %v\n", err)
	} else if walPages > 1000 { // 如果 WAL 文件超过 1000 页
		fmt.Printf("WAL 文件较大 (%d 页)，执行 checkpoint\n", walPages)
	}
	
	// 2. 执行 WAL checkpoint (TRUNCATE 模式会截断 WAL 文件)
	var busy, log, checkpointed int
	err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed)
	if err != nil {
		fmt.Printf("WAL checkpoint 失败: %v\n", err)
	} else {
		fmt.Printf("WAL checkpoint 完成: busy=%d, log=%d, checkpointed=%d\n", busy, log, checkpointed)
	}
	
	// 3. 优化数据库（重新分析统计信息）
	_, err = db.Exec("PRAGMA optimize")
	if err != nil {
		fmt.Printf("数据库优化失败: %v\n", err)
	} else {
		fmt.Println("数据库优化完成")
	}
	
	// 4. 检查数据库完整性（可选，较耗时）
	// 建议在低峰期或每天执行一次
	/*
	var integrityOk string
	err = db.QueryRow("PRAGMA integrity_check").Scan(&integrityOk)
	if err != nil {
		fmt.Printf("完整性检查失败: %v\n", err)
	} else if integrityOk != "ok" {
		fmt.Printf("数据库完整性问题: %s\n", integrityOk)
	}
	*/
}

// 使用示例：在您的主函数中添加
/*
func main() {
	// ... 其他初始化代码 ...
	
	// 启动 SQLite 维护任务
	maintenance := NewSQLiteMaintenanceManager("GEServer.db")
	maintenance.Start()
	
	// 确保在程序退出时停止维护任务
	defer maintenance.Stop()
	
	// ... 启动 Fiber 服务器等 ...
}
*/