package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

const (
	// 默认分页参数
	defaultPage       = 1
	defaultLimit      = 10
	defaultSortField  = "expiry"
	defaultSortOrder  = "asc"
	
	// 用于排序的特殊值
	emptyUsernameFallback = "zzzzzzzz" // 空用户名排最后
	
	// 时间格式
	timeFormat = time.RFC3339
)

// SessionItem 会话项结构
type SessionItem map[string]interface{}

// convertInterfaceMapToStringMap 将 map[interface{}]interface{} 转换为 map[string]interface{}
func convertInterfaceMapToStringMap(rawData map[interface{}]interface{}) map[string]interface{} {
	data := make(map[string]interface{}, len(rawData))
	for key, value := range rawData {
		if strKey, ok := key.(string); ok {
			data[strKey] = value
		} else {
			// 如果key不是string类型，尝试转换为string
			data[fmt.Sprintf("%v", key)] = value
		}
	}
	return data
}

// decodeSessionData 解码会话数据
func decodeSessionData(v []byte) (map[string]interface{}, error) {
	rawData := make(map[interface{}]interface{})
	buf := bytes.NewBuffer(v)
	dec := gob.NewDecoder(buf)
	
	if err := dec.Decode(&rawData); err != nil {
		return nil, fmt.Errorf("gob decode failed: %w", err)
	}
	
	return convertInterfaceMapToStringMap(rawData), nil
}

// extractSessionDataFromMemory 从内存session中提取数据
func extractSessionDataFromMemory(sessionData interface{}) (map[string]interface{}, error) {
	switch data := sessionData.(type) {
	case map[string]interface{}:
		// 如果已经是目标格式，直接返回
		return data, nil
	case map[interface{}]interface{}:
		// 转换接口类型的map
		return convertInterfaceMapToStringMap(data), nil
	case []byte:
		// 如果是字节数组，尝试gob解码
		return decodeSessionData(data)
	default:
		// 其他类型，尝试通过反射获取字段
		result := make(map[string]interface{})
		// 这里可以根据实际的session结构进行调整
		// 如果无法直接转换，返回空map
		return result, nil
	}
}

// createSessionItem 创建会话项
func createSessionItem(expiry int64, key string, data map[string]interface{}) SessionItem {
	sessionItem := SessionItem{
		"expiry":    expiry,
		"key":       key,
		"expiryStr": time.Unix(expiry, 0).Format(timeFormat),
	}
	
	// 将data中的所有字段合并到外层
	for k, v := range data {
		sessionItem[k] = v
	}
	
	// 确保username字段存在（用于过滤和排序）
	if _, exists := sessionItem["username"]; !exists {
		sessionItem["username"] = ""
	}
	
	return sessionItem
}

// filterByUsername 根据用户名过滤会话
func filterByUsername(sessionItem SessionItem, rawUsernameFilter string) bool {
	if rawUsernameFilter == "" {
		return true
	}
	
	username, ok := sessionItem["username"].(string)
	if !ok {
		username = ""
	}
	
	usernameLower := strings.ToLower(username)
	usernameFilter := strings.ToLower(rawUsernameFilter)
	
	return strings.Contains(usernameLower, usernameFilter)
}

// sortSessions 对会话列表进行排序
func sortSessions(sessions []SessionItem, sortField, sortOrder string) {
	sort.Slice(sessions, func(i, j int) bool {
		switch sortField {
		case "expiry":
			return compareByExpiry(sessions[i], sessions[j], sortOrder)
		default: // 默认按username排序
			return compareByUsername(sessions[i], sessions[j], sortOrder)
		}
	})
}

// compareByExpiry 按到期时间比较
func compareByExpiry(a, b SessionItem, sortOrder string) bool {
	expiryA, _ := a["expiry"].(int64)
	expiryB, _ := b["expiry"].(int64)
	
	if sortOrder == "asc" {
		return expiryA < expiryB
	}
	return expiryA > expiryB
}

// compareByUsername 按用户名比较
func compareByUsername(a, b SessionItem, sortOrder string) bool {
	userA, ok1 := a["username"].(string)
	userB, ok2 := b["username"].(string)
	
	// 处理空用户名的情况
	if !ok1 || userA == "" {
		userA = emptyUsernameFallback
	}
	if !ok2 || userB == "" {
		userB = emptyUsernameFallback
	}
	
	// 使用小写进行比较
	userALower := strings.ToLower(userA)
	userBLower := strings.ToLower(userB)
	
	if sortOrder == "asc" {
		return userALower < userBLower
	}
	return userALower > userBLower
}

// paginateSessions 对会话列表进行分页
func paginateSessions(sessions []SessionItem, page, limit int) []SessionItem {
	total := len(sessions)
	start := (page - 1) * limit
	
	// 边界检查
	if start < 0 {
		start = 0
	}
	if start >= total {
		return []SessionItem{}
	}
	
	end := start + limit
	if end > total {
		end = total
	}
	
	return sessions[start:end]
}

// validateAndNormalizePagination 验证并规范化分页参数
func validateAndNormalizePagination(page, limit int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if limit < 1 {
		limit = defaultLimit
	}
	return page, limit
}

func GetOnlineUser(c CtxHelper) error {
	// 获取会话
	sess, err := US.Get(c)
	if err != nil {
		return c.Fail(err.Error())
	}
	defer sess.Release()
	
	fmt.Printf("当前ID为：%s \n", sess.ID())

	// 获取查询参数
	rawUsernameFilter := c.Ctx.Query("username")
	page := fiber.Query[int](c, "page", defaultPage)
	limit := fiber.Query[int](c, "limit", defaultLimit)
	sortField := fiber.Query[string](c, "sortField", defaultSortField)
	sortOrder := fiber.Query[string](c, "sortOrder", defaultSortOrder)

	// 验证分页参数
	page, limit = validateAndNormalizePagination(page, limit)

	// 从内存存储获取所有session数据
	// 假设Storage有一个方法可以获取所有session
	// 这里需要根据你实际使用的内存存储来调整
	var allSessions []SessionItem
	
	// 方法1: 如果Storage有GetAll方法
	if sessions, err := Storage.GetAll(); err != nil {
		return c.Fail(err.Error())
	} else {
		for sessionKey, sessionData := range sessions {
			// 获取session的过期时间
			var expiry int64
			var sessionDataMap map[string]interface{}
			
			// 根据你的存储结构调整这里的逻辑
			// 如果session存储包含过期时间信息
			if sessInfo, ok := sessionData.(map[string]interface{}); ok {
				if exp, exists := sessInfo["expiry"]; exists {
					if expTime, ok := exp.(int64); ok {
						expiry = expTime
					} else if expTime, ok := exp.(time.Time); ok {
						expiry = expTime.Unix()
					}
				}
				sessionDataMap = sessInfo
			} else {
				// 如果没有过期时间信息，可以设置一个默认值或者跳过
				expiry = time.Now().Add(24 * time.Hour).Unix() // 默认24小时后过期
				// 尝试提取session数据
				if extractedData, err := extractSessionDataFromMemory(sessionData); err == nil {
					sessionDataMap = extractedData
				} else {
					log.Printf("错误_ExtractSessionData：%s", err.Error())
					continue
				}
			}
			
			fmt.Printf("v：%v \n", sessionDataMap)
			
			// 创建会话项
			sessionItem := createSessionItem(expiry, sessionKey, sessionDataMap)
			
			// 用户名过滤
			if !filterByUsername(sessionItem, rawUsernameFilter) {
				continue
			}
			
			// 输出当前用户名（调试用）
			if username, ok := sessionItem["username"].(string); ok {
				fmt.Printf("当前：%v\n", username)
			}
			
			// 注释掉的admin过滤逻辑保持不变
			// usernameLower := strings.ToLower(username)
			// if usernameLower == "admin" {
			//     continue
			// }
			
			allSessions = append(allSessions, sessionItem)
		}
	}
	
	// 方法2: 如果Storage是fiber的session存储，可能需要这样访问
	/*
	if store, ok := Storage.(*memory.Storage); ok {
		store.Range(func(key, value interface{}) bool {
			sessionKey := fmt.Sprintf("%v", key)
			
			// 处理session数据
			sessionDataMap, err := extractSessionDataFromMemory(value)
			if err != nil {
				log.Printf("错误_ExtractSessionData：%s", err.Error())
				return true // 继续遍历
			}
			
			// 获取过期时间（需要根据实际存储结构调整）
			expiry := time.Now().Add(24 * time.Hour).Unix()
			
			fmt.Printf("v：%v \n", sessionDataMap)
			
			// 创建会话项
			sessionItem := createSessionItem(expiry, sessionKey, sessionDataMap)
			
			// 用户名过滤
			if !filterByUsername(sessionItem, rawUsernameFilter) {
				return true // 继续遍历
			}
			
			// 输出当前用户名（调试用）
			if username, ok := sessionItem["username"].(string); ok {
				fmt.Printf("当前：%v\n", username)
			}
			
			allSessions = append(allSessions, sessionItem)
			return true // 继续遍历
		})
	}
	*/

	// 排序
	sortSessions(allSessions, sortField, sortOrder)
	
	// 分页
	total := len(allSessions)
	pagedSessions := paginateSessions(allSessions, page, limit)
	
	// 确保返回数组而非nil
	if pagedSessions == nil {
		pagedSessions = []SessionItem{}
	}

	// 返回结果
	return c.JSON(fiber.Map{
		"code": 0,
		"message": fiber.Map{
			"list":  pagedSessions,
			"count": total,
			"times": 0,
			// 注释掉的部分保持不变
			//"sort": fiber.Map{
			//	"field": sortField,
			//	"order": sortOrder,
			//},
			//"page": fiber.Map{
			//	"current": page,
			//	"size":    limit,
			//	"total":   total,
			//},
		},
	})
}