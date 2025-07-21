package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/storage/sqlite3/v2"
	_ "github.com/mattn/go-sqlite3"
)

// SessionData 表示一个session记录
type SessionData struct {
	Key      string `json:"key"`
	Value    string `json:"value"`    // 原始的二进制数据转换为base64或十六进制
	RawValue []byte `json:"-"`        // 原始二进制数据，不在JSON中显示
	Expires  int64  `json:"expires"`  // 过期时间戳
	IsExpired bool  `json:"is_expired"` // 是否已过期
}

// ReadAllSessionData 从 sys.Storage 中读取所有session数据并返回JSON数组
func ReadAllSessionData(storage *sqlite3.Storage) ([]byte, error) {
	// 获取底层数据库连接
	db := storage.Conn()
	
	// 查询所有session数据
	// 根据GoFiber SQLite3存储的表结构：k (key), v (value), e (expires)
	query := "SELECT k, v, e FROM sessions ORDER BY k"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("查询数据失败: %v", err)
	}
	defer rows.Close()

	var sessions []SessionData
	currentTime := time.Now().Unix()

	// 遍历所有行
	for rows.Next() {
		var session SessionData
		err := rows.Scan(&session.Key, &session.RawValue, &session.Expires)
		if err != nil {
			log.Printf("扫描行数据时出错: %v", err)
			continue
		}

		// 检查是否过期
		session.IsExpired = session.Expires != 0 && session.Expires <= currentTime

		// 将二进制数据转换为字符串（这里使用字符串形式，你也可以用base64）
		// 如果数据是可打印的字符串，直接转换；否则可以使用base64编码
		session.Value = string(session.RawValue)

		sessions = append(sessions, session)
	}

	// 检查遍历过程中是否有错误
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历数据时出错: %v", err)
	}

	// 转换为JSON
	jsonData, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("转换为JSON失败: %v", err)
	}

	return jsonData, nil
}

// ReadAllSessionDataAsBase64 读取所有session数据，将value编码为base64
func ReadAllSessionDataAsBase64(storage *sqlite3.Storage) ([]byte, error) {
	// 获取底层数据库连接
	db := storage.Conn()
	
	query := "SELECT k, v, e FROM sessions ORDER BY k"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("查询数据失败: %v", err)
	}
	defer rows.Close()

	var sessions []map[string]interface{}
	currentTime := time.Now().Unix()

	for rows.Next() {
		var key string
		var value []byte
		var expires int64
		
		err := rows.Scan(&key, &value, &expires)
		if err != nil {
			log.Printf("扫描行数据时出错: %v", err)
			continue
		}

		session := map[string]interface{}{
			"key":         key,
			"value_raw":   string(value), // 原始字符串形式
			"value_hex":   fmt.Sprintf("%x", value), // 十六进制形式
			"expires":     expires,
			"is_expired":  expires != 0 && expires <= currentTime,
			"expires_at":  "",
		}

		// 如果有过期时间，转换为可读的时间格式
		if expires != 0 {
			session["expires_at"] = time.Unix(expires, 0).Format("2006-01-02 15:04:05")
		}

		sessions = append(sessions, session)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历数据时出错: %v", err)
	}

	jsonData, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("转换为JSON失败: %v", err)
	}

	return jsonData, nil
}

// GetSessionCount 获取session总数
func GetSessionCount(storage *sqlite3.Storage) (int, error) {
	db := storage.Conn()
	
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询session数量失败: %v", err)
	}
	
	return count, nil
}

// GetActiveSessionCount 获取未过期的session数量
func GetActiveSessionCount(storage *sqlite3.Storage) (int, error) {
	db := storage.Conn()
	
	var count int
	currentTime := time.Now().Unix()
	err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE e = 0 OR e > ?", currentTime).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询活跃session数量失败: %v", err)
	}
	
	return count, nil
}

// 示例使用函数
func ExampleUsage() {
	// 假设你已经有了 sys.Storage
	// storage := sys.Storage
	
	// 创建一个示例storage（实际使用时请使用你的 sys.Storage）
	storage := sqlite3.New(sqlite3.Config{
		Database: "GEServer.db?_journal_mode=WAL",
		Table:    "sessions",
	})
	defer storage.Close()

	// 读取所有session数据
	jsonData, err := ReadAllSessionDataAsBase64(storage)
	if err != nil {
		log.Fatalf("读取session数据失败: %v", err)
	}

	// 打印JSON数据
	fmt.Println("所有Session数据:")
	fmt.Println(string(jsonData))

	// 获取统计信息
	totalCount, err := GetSessionCount(storage)
	if err != nil {
		log.Printf("获取总数失败: %v", err)
	} else {
		fmt.Printf("\n总Session数量: %d\n", totalCount)
	}

	activeCount, err := GetActiveSessionCount(storage)
	if err != nil {
		log.Printf("获取活跃数量失败: %v", err)
	} else {
		fmt.Printf("活跃Session数量: %d\n", activeCount)
	}
}

// 如果你想在现有代码中集成，可以这样使用：
func IntegrateWithYourCode() {
	// 在你的代码中，假设你已经创建了 sys.Storage
	// 你可以这样调用：
	
	/*
	// 读取所有session数据
	jsonData, err := ReadAllSessionDataAsBase64(sys.Storage)
	if err != nil {
		log.Printf("读取session数据失败: %v", err)
		return
	}
	
	// 将JSON数据写入文件
	err = ioutil.WriteFile("sessions.json", jsonData, 0644)
	if err != nil {
		log.Printf("写入文件失败: %v", err)
		return
	}
	
	fmt.Println("Session数据已导出到 sessions.json")
	
	// 或者直接返回给API调用者
	// c.JSON(fiber.Map{"sessions": json.RawMessage(jsonData)})
	*/
}