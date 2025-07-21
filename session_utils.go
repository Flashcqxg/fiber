package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gofiber/storage/sqlite3/v2"
)

// SessionRecord 表示session记录的结构
type SessionRecord struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueHex  string `json:"value_hex,omitempty"`
	Expires   int64  `json:"expires"`
	ExpiresAt string `json:"expires_at,omitempty"`
	IsExpired bool   `json:"is_expired"`
}

// ExportAllSessions 导出所有session数据为JSON
// 使用您现有的 sys.Storage
func ExportAllSessions(storage *sqlite3.Storage) ([]byte, error) {
	// 获取数据库连接
	db := storage.Conn()
	
	// 查询所有session数据
	query := "SELECT k, v, e FROM sessions ORDER BY k"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("查询session数据失败: %v", err)
	}
	defer rows.Close()

	var sessions []SessionRecord
	currentTime := time.Now().Unix()

	// 遍历查询结果
	for rows.Next() {
		var key string
		var value []byte
		var expires int64
		
		if err := rows.Scan(&key, &value, &expires); err != nil {
			log.Printf("读取session记录失败: %v", err)
			continue
		}

		// 创建session记录
		record := SessionRecord{
			Key:       key,
			Value:     string(value),
			ValueHex:  fmt.Sprintf("%x", value),
			Expires:   expires,
			IsExpired: expires != 0 && expires <= currentTime,
		}

		// 添加可读的过期时间
		if expires != 0 {
			record.ExpiresAt = time.Unix(expires, 0).Format("2006-01-02 15:04:05")
		}

		sessions = append(sessions, record)
	}

	// 检查是否有遍历错误
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历session数据时出错: %v", err)
	}

	// 转换为格式化的JSON
	jsonData, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("转换JSON失败: %v", err)
	}

	return jsonData, nil
}

// SaveSessionsToFile 将session数据保存到文件
func SaveSessionsToFile(storage *sqlite3.Storage, filename string) error {
	jsonData, err := ExportAllSessions(storage)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, jsonData, 0644)
}

// GetSessionStatistics 获取session统计信息
func GetSessionStatistics(storage *sqlite3.Storage) (map[string]interface{}, error) {
	db := storage.Conn()
	currentTime := time.Now().Unix()

	stats := make(map[string]interface{})

	// 总session数
	var total int
	err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("获取总数失败: %v", err)
	}
	stats["total"] = total

	// 活跃session数（未过期的）
	var active int
	err = db.QueryRow("SELECT COUNT(*) FROM sessions WHERE e = 0 OR e > ?", currentTime).Scan(&active)
	if err != nil {
		return nil, fmt.Errorf("获取活跃数失败: %v", err)
	}
	stats["active"] = active

	// 过期session数
	stats["expired"] = total - active

	// 永不过期的session数
	var neverExpire int
	err = db.QueryRow("SELECT COUNT(*) FROM sessions WHERE e = 0").Scan(&neverExpire)
	if err != nil {
		return nil, fmt.Errorf("获取永不过期数失败: %v", err)
	}
	stats["never_expire"] = neverExpire

	return stats, nil
}

// 使用示例：
// func main() {
//     // 假设你已经有了 sys.Storage 和 sys.US
//     
//     // 方法1: 导出所有session数据到JSON
//     jsonData, err := ExportAllSessions(sys.Storage)
//     if err != nil {
//         log.Printf("导出失败: %v", err)
//         return
//     }
//     fmt.Println("Session数据JSON:")
//     fmt.Println(string(jsonData))
//     
//     // 方法2: 保存到文件
//     err = SaveSessionsToFile(sys.Storage, "sessions_export.json")
//     if err != nil {
//         log.Printf("保存文件失败: %v", err)
//     } else {
//         fmt.Println("Session数据已保存到 sessions_export.json")
//     }
//     
//     // 方法3: 获取统计信息
//     stats, err := GetSessionStatistics(sys.Storage)
//     if err != nil {
//         log.Printf("获取统计失败: %v", err)
//     } else {
//         fmt.Printf("Session统计: %+v\n", stats)
//     }
// }