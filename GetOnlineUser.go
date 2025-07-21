package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
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

// MemorySession 内存session数据结构（需要根据实际结构调整）
type MemorySession struct {
	Data   map[string]interface{}
	Expiry time.Time
}

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

// getAllSessionsFromStore 从session store中获取所有session
func getAllSessionsFromStore(store interface{}) (map[string]SessionItem, error) {
	sessions := make(map[string]SessionItem)
	
	// 方法1: 通过反射访问内存存储的内部数据
	storeValue := reflect.ValueOf(store)
	if storeValue.Kind() == reflect.Ptr {
		storeValue = storeValue.Elem()
	}
	
	// 尝试查找存储session的字段（通常是sync.Map或map类型）
	for i := 0; i < storeValue.NumField(); i++ {
		field := storeValue.Field(i)
		fieldType := storeValue.Type().Field(i)
		
		// 查找可能存储session的字段
		if strings.Contains(strings.ToLower(fieldType.Name), "session") ||
		   strings.Contains(strings.ToLower(fieldType.Name), "data") ||
		   strings.Contains(strings.ToLower(fieldType.Name), "store") {
			
			if field.Kind() == reflect.Map {
				// 如果是普通map
				for _, key := range field.MapKeys() {
					value := field.MapIndex(key)
					sessionKey := fmt.Sprintf("%v", key.Interface())
					
					if sessionItem, err := processSessionData(sessionKey, value.Interface()); err == nil {
						sessions[sessionKey] = sessionItem
					}
				}
			} else if field.Type().String() == "sync.Map" {
				// 如果是sync.Map，使用unsafe包访问
				if syncMap, ok := field.Interface().(*sync.Map); ok {
					syncMap.Range(func(key, value interface{}) bool {
						sessionKey := fmt.Sprintf("%v", key)
						if sessionItem, err := processSessionData(sessionKey, value); err == nil {
							sessions[sessionKey] = sessionItem
						}
						return true
					})
				}
			}
		}
	}
	
	return sessions, nil
}

// processSessionData 处理单个session数据
func processSessionData(sessionKey string, sessionData interface{}) (SessionItem, error) {
	var expiry int64
	var sessionDataMap map[string]interface{}
	
	// 尝试从session数据中提取信息
	switch data := sessionData.(type) {
	case map[string]interface{}:
		sessionDataMap = data
		// 查找过期时间
		if exp, exists := data["expiry"]; exists {
			if expTime, ok := exp.(int64); ok {
				expiry = expTime
			} else if expTime, ok := exp.(time.Time); ok {
				expiry = expTime.Unix()
			}
		}
	case *MemorySession:
		sessionDataMap = data.Data
		expiry = data.Expiry.Unix()
	default:
		// 尝试通过反射获取数据
		val := reflect.ValueOf(sessionData)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		
		sessionDataMap = make(map[string]interface{})
		
		// 查找Data字段
		if dataField := val.FieldByName("Data"); dataField.IsValid() {
			if dataMap, ok := dataField.Interface().(map[string]interface{}); ok {
				sessionDataMap = dataMap
			}
		}
		
		// 查找Expiry字段
		if expiryField := val.FieldByName("Expiry"); expiryField.IsValid() {
			if expiryTime, ok := expiryField.Interface().(time.Time); ok {
				expiry = expiryTime.Unix()
			}
		}
	}
	
	// 如果没有找到过期时间，设置默认值
	if expiry == 0 {
		expiry = time.Now().Add(24 * time.Hour).Unix()
	}
	
	return createSessionItem(expiry, sessionKey, sessionDataMap), nil
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

	// 从session store获取所有session数据
	var allSessionsSlice []SessionItem
	
	// 方法1: 通过US (session管理器) 获取底层存储
	// 假设US有Store()方法返回底层存储
	if store := US.Store(); store != nil {
		if sessions, err := getAllSessionsFromStore(store); err == nil {
			for _, sessionItem := range sessions {
				// 用户名过滤
				if !filterByUsername(sessionItem, rawUsernameFilter) {
					continue
				}
				
				// 输出当前用户名（调试用）
				if username, ok := sessionItem["username"].(string); ok {
					fmt.Printf("当前：%v\n", username)
				}
				
				fmt.Printf("v：%v \n", sessionItem)
				
				// 注释掉的admin过滤逻辑保持不变
				// usernameLower := strings.ToLower(username)
				// if usernameLower == "admin" {
				//     continue
				// }
				
				allSessionsSlice = append(allSessionsSlice, sessionItem)
			}
		} else {
			log.Printf("获取sessions失败：%s", err.Error())
		}
	}
	
	// 方法2: 如果方法1不可用，尝试其他方式
	// 这里可以添加其他获取session的方法
	/*
	// 例如：如果有全局的session映射
	if globalSessions != nil {
		globalSessions.Range(func(key, value interface{}) bool {
			sessionKey := fmt.Sprintf("%v", key)
			if sessionItem, err := processSessionData(sessionKey, value); err == nil {
				if filterByUsername(sessionItem, rawUsernameFilter) {
					allSessionsSlice = append(allSessionsSlice, sessionItem)
				}
			}
			return true
		})
	}
	*/

	// 排序
	sortSessions(allSessionsSlice, sortField, sortOrder)
	
	// 分页
	total := len(allSessionsSlice)
	pagedSessions := paginateSessions(allSessionsSlice, page, limit)
	
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