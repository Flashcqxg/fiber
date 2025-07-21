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

// extractAllSessionsFromStore 从Fiber v3 session store中提取所有session数据
func extractAllSessionsFromStore(store interface{}) (map[string]SessionItem, error) {
	sessions := make(map[string]SessionItem)
	
	storeValue := reflect.ValueOf(store)
	if storeValue.Kind() == reflect.Ptr {
		storeValue = storeValue.Elem()
	}
	
	// 查找Storage字段（Fiber v3中session store包含Storage字段）
	storageField := storeValue.FieldByName("Storage")
	if !storageField.IsValid() {
		return sessions, fmt.Errorf("Storage field not found in session store")
	}
	
	// 获取Storage接口的实际实现
	storage := storageField.Interface()
	storageValue := reflect.ValueOf(storage)
	if storageValue.Kind() == reflect.Ptr {
		storageValue = storageValue.Elem()
	}
	
	// 对于memory storage，查找存储数据的内部字段
	// 通常是sync.Map或map类型
	err := traverseStorageFields(storageValue, sessions)
	if err != nil {
		return sessions, fmt.Errorf("failed to traverse storage fields: %w", err)
	}
	
	return sessions, nil
}

// traverseStorageFields 遍历storage的字段查找session数据
func traverseStorageFields(storageValue reflect.Value, sessions map[string]SessionItem) error {
	storageType := storageValue.Type()
	
	for i := 0; i < storageValue.NumField(); i++ {
		field := storageValue.Field(i)
		fieldType := storageType.Field(i)
		
		// 跳过未导出的字段，使用unsafe包访问
		if !fieldType.IsExported() {
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
		}
		
		// 查找可能存储session数据的字段
		fieldName := strings.ToLower(fieldType.Name)
		if strings.Contains(fieldName, "data") || 
		   strings.Contains(fieldName, "store") || 
		   strings.Contains(fieldName, "session") ||
		   strings.Contains(fieldName, "map") {
			
			if err := extractFromField(field, sessions); err != nil {
				log.Printf("Error extracting from field %s: %v", fieldType.Name, err)
				continue
			}
		}
	}
	
	return nil
}

// extractFromField 从字段中提取session数据
func extractFromField(field reflect.Value, sessions map[string]SessionItem) error {
	switch field.Kind() {
	case reflect.Map:
		// 处理普通map
		return extractFromMap(field, sessions)
		
	case reflect.Interface, reflect.Ptr:
		if field.IsNil() {
			return nil
		}
		// 如果是接口或指针，获取实际值
		actual := field.Elem()
		if actual.Kind() == reflect.Map {
			return extractFromMap(actual, sessions)
		}
		
		// 检查是否是sync.Map
		if field.Type().String() == "*sync.Map" {
			return extractFromSyncMap(field, sessions)
		}
		
	case reflect.Struct:
		// 如果是sync.Map结构体
		if field.Type().String() == "sync.Map" {
			syncMapPtr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
			return extractFromSyncMap(syncMapPtr, sessions)
		}
	}
	
	return nil
}

// extractFromMap 从普通map中提取session数据
func extractFromMap(mapValue reflect.Value, sessions map[string]SessionItem) error {
	for _, key := range mapValue.MapKeys() {
		value := mapValue.MapIndex(key)
		sessionKey := fmt.Sprintf("%v", key.Interface())
		
		if sessionItem, err := processSessionValue(sessionKey, value.Interface()); err == nil {
			sessions[sessionKey] = sessionItem
		} else {
			log.Printf("Error processing session %s: %v", sessionKey, err)
		}
	}
	return nil
}

// extractFromSyncMap 从sync.Map中提取session数据
func extractFromSyncMap(syncMapPtr reflect.Value, sessions map[string]SessionItem) error {
	if syncMap, ok := syncMapPtr.Interface().(*sync.Map); ok {
		syncMap.Range(func(key, value interface{}) bool {
			sessionKey := fmt.Sprintf("%v", key)
			if sessionItem, err := processSessionValue(sessionKey, value); err == nil {
				sessions[sessionKey] = sessionItem
			} else {
				log.Printf("Error processing session %s: %v", sessionKey, err)
			}
			return true
		})
	}
	return nil
}

// processSessionValue 处理session值
func processSessionValue(sessionKey string, sessionValue interface{}) (SessionItem, error) {
	var expiry int64
	var sessionDataMap map[string]interface{}
	
	// 处理不同类型的session数据
	switch data := sessionValue.(type) {
	case []byte:
		// 如果是字节数组，尝试gob解码
		if decoded, err := decodeSessionData(data); err == nil {
			sessionDataMap = decoded
		} else {
			return nil, fmt.Errorf("failed to decode session data: %w", err)
		}
		
	case map[string]interface{}:
		sessionDataMap = data
		
	case map[interface{}]interface{}:
		sessionDataMap = convertInterfaceMapToStringMap(data)
		
	default:
		// 尝试通过反射处理结构体
		val := reflect.ValueOf(sessionValue)
		if val.Kind() == reflect.Ptr {
			if val.IsNil() {
				return nil, fmt.Errorf("nil session value")
			}
			val = val.Elem()
		}
		
		if val.Kind() == reflect.Struct {
			sessionDataMap = make(map[string]interface{})
			
			// 查找Data字段
			if dataField := val.FieldByName("Data"); dataField.IsValid() {
				if dataMap, ok := dataField.Interface().(map[string]interface{}); ok {
					sessionDataMap = dataMap
				} else if dataMap, ok := dataField.Interface().(map[interface{}]interface{}); ok {
					sessionDataMap = convertInterfaceMapToStringMap(dataMap)
				}
			}
			
			// 查找Expiry或Exp字段
			if expiryField := val.FieldByName("Expiry"); expiryField.IsValid() {
				if expiryTime, ok := expiryField.Interface().(time.Time); ok {
					expiry = expiryTime.Unix()
				}
			} else if expField := val.FieldByName("Exp"); expField.IsValid() {
				if expTime, ok := expField.Interface().(int64); ok {
					expiry = expTime
				} else if expTime, ok := expField.Interface().(time.Time); ok {
					expiry = expTime.Unix()
				}
			}
		} else {
			return nil, fmt.Errorf("unsupported session value type: %T", sessionValue)
		}
	}
	
	// 尝试从sessionDataMap中获取过期时间
	if expiry == 0 {
		if exp, exists := sessionDataMap["expiry"]; exists {
			if expTime, ok := exp.(int64); ok {
				expiry = expTime
			} else if expTime, ok := exp.(time.Time); ok {
				expiry = expTime.Unix()
			}
		} else if exp, exists := sessionDataMap["exp"]; exists {
			if expTime, ok := exp.(int64); ok {
				expiry = expTime
			} else if expTime, ok := exp.(time.Time); ok {
				expiry = expTime.Unix()
			}
		}
	}
	
	// 如果仍然没有过期时间，设置默认值
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
	
	// 从US session store中提取所有session
	if sessions, err := extractAllSessionsFromStore(US); err == nil {
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
		// 如果提取失败，返回空列表而不是错误
		allSessionsSlice = []SessionItem{}
	}

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