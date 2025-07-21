package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// 简化版本：直接复制这个函数到您的项目中使用
func ExportSessionsFromStorage(storage interface{}) (string, error) {
	// 类型断言，获取 sqlite3.Storage
	sqliteStorage, ok := storage.(*sqlite3.Storage)
	if !ok {
		return "", fmt.Errorf("storage 类型不正确，需要 *sqlite3.Storage")
	}
	
	// 获取数据库连接
	db := sqliteStorage.Conn()
	
	// 查询所有session数据
	rows, err := db.Query("SELECT k, v, e FROM sessions ORDER BY k")
	if err != nil {
		return "", fmt.Errorf("查询失败: %v", err)
	}
	defer rows.Close()

	var result []map[string]interface{}
	currentTime := time.Now().Unix()

	for rows.Next() {
		var key string
		var value []byte
		var expires int64
		
		if err := rows.Scan(&key, &value, &expires); err != nil {
			continue // 跳过错误的行
		}

		record := map[string]interface{}{
			"key":         key,
			"value":       string(value),
			"expires":     expires,
			"is_expired":  expires != 0 && expires <= currentTime,
		}

		// 添加可读的过期时间
		if expires != 0 {
			record["expires_at"] = time.Unix(expires, 0).Format("2006-01-02 15:04:05")
		} else {
			record["expires_at"] = "永不过期"
		}

		result = append(result, record)
	}

	// 转换为JSON字符串
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON转换失败: %v", err)
	}

	return string(jsonBytes), nil
}

// 使用示例 - 直接在您的代码中这样调用：
/*
func main() {
    // 假设您已经创建了 sys.Storage
    sys.Storage = sqlite3.New(sqlite3.Config{
        Database: "GEServer.db?_journal_mode=WAL",
        Table:    "sessions",
    })
    
    // 导出所有session数据
    jsonResult, err := ExportSessionsFromStorage(sys.Storage)
    if err != nil {
        fmt.Printf("导出失败: %v\n", err)
        return
    }
    
    fmt.Println("所有Session数据:")
    fmt.Println(jsonResult)
    
    // 如果要保存到文件
    err = ioutil.WriteFile("sessions.json", []byte(jsonResult), 0644)
    if err != nil {
        fmt.Printf("保存文件失败: %v\n", err)
    } else {
        fmt.Println("数据已保存到 sessions.json")
    }
}
*/

// 如果您想要更简单的版本，只返回基本数据：
func GetAllSessionsSimple(storage interface{}) ([]map[string]string, error) {
	sqliteStorage, ok := storage.(*sqlite3.Storage)
	if !ok {
		return nil, fmt.Errorf("storage 类型错误")
	}
	
	db := sqliteStorage.Conn()
	rows, err := db.Query("SELECT k, v, e FROM sessions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []map[string]string
	for rows.Next() {
		var key string
		var value []byte
		var expires int64
		
		if err := rows.Scan(&key, &value, &expires); err != nil {
			continue
		}

		session := map[string]string{
			"key":   key,
			"value": string(value),
		}
		
		if expires != 0 {
			session["expires"] = time.Unix(expires, 0).Format("2006-01-02 15:04:05")
		} else {
			session["expires"] = "永不过期"
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}