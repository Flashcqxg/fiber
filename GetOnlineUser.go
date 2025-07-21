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

// 缓存结构体类型信息以提高性能
var (
	sessionStoreCacheOnce sync.Once
	sessionStorageField   *reflect.StructField
	storageTypeCache      sync.Map // 存储不同storage类型的字段信息
)

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

// 快速路径：直接尝试常见的session访问方式
func fastExtractSessions(store interface{}) (map[string]SessionItem, bool) {
	sessions := make(map[string]SessionItem)
	
	// 尝试最常见的结构 - 直接访问Storage字段
	storeValue := reflect.ValueOf(store)
	if storeValue.Kind() == reflect.Ptr {
		storeValue = storeValue.Elem()
	}
	
	// 缓存Storage字段信息
	sessionStoreCacheOnce.Do(func() {
		storeType := storeValue.Type()
		for i := 0; i < storeType.NumField(); i++ {
			field := storeType.Field(i)
			if field.Name == "Storage" {
				sessionStorageField = &field
				break
			}
		}
	})
	
	if sessionStorageField != nil {
		storageField := storeValue.FieldByName("Storage")
		if storageField.IsValid() && !storageField.IsNil() {
			storage := storageField.Interface()
			if extracted := fastExtractFromStorage(storage); len(extracted) > 0 {
				return extracted, true
			}
		}
	}
	
	return sessions, false
}

// 快速从storage中提取session
func fastExtractFromStorage(storage interface{}) map[string]SessionItem {
	sessions := make(map[string]SessionItem)
	
	storageValue := reflect.ValueOf(storage)
	if storageValue.Kind() == reflect.Ptr {
		storageValue = storageValue.Elem()
	}
	
	storageType := storageValue.Type()
	cacheKey := storageType.String()
	
	// 检查缓存
	if cached, ok := storageTypeCache.Load(cacheKey); ok {
		if fieldInfo, ok := cached.([]int); ok {
			for _, fieldIndex := range fieldInfo {
				field := storageValue.Field(fieldIndex)
				if fastExtractFromField(field, sessions) {
					break // 找到数据就退出
				}
			}
			return sessions
		}
	}
	
	// 第一次访问，分析字段并缓存
	var validFields []int
	for i := 0; i < storageValue.NumField(); i++ {
		fieldType := storageType.Field(i)
		fieldName := strings.ToLower(fieldType.Name)
		
		// 查找可能包含session数据的字段
		if strings.Contains(fieldName, "data") || 
		   strings.Contains(fieldName, "store") || 
		   strings.Contains(fieldName, "session") ||
		   strings.Contains(fieldName, "map") ||
		   fieldName == "db" || fieldName == "storage" {
			validFields = append(validFields, i)
		}
	}
	
	// 缓存字段信息
	storageTypeCache.Store(cacheKey, validFields)
	
	// 尝试从这些字段中提取数据
	for _, fieldIndex := range validFields {
		field := storageValue.Field(fieldIndex)
		if fastExtractFromField(field, sessions) {
			break // 找到数据就退出
		}
	}
	
	return sessions
}

// 快速从字段中提取session数据
func fastExtractFromField(field reflect.Value, sessions map[string]SessionItem) bool {
	// 处理未导出字段
	if !field.CanInterface() {
		if field.CanAddr() {
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
		} else {
			return false
		}
	}
	
	switch field.Kind() {
	case reflect.Map:
		if field.IsNil() {
			return false
		}
		return extractFromMapFast(field, sessions)
		
	case reflect.Interface, reflect.Ptr:
		if field.IsNil() {
			return false
		}
		actual := field.Elem()
		if actual.Kind() == reflect.Map {
			return extractFromMapFast(actual, sessions)
		}
		
		// 检查sync.Map
		if field.Type().String() == "*sync.Map" || 
		   strings.Contains(field.Type().String(), "sync.Map") {
			return extractFromSyncMapFast(field, sessions)
		}
		
	case reflect.Struct:
		if field.Type().String() == "sync.Map" {
			syncMapPtr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
			return extractFromSyncMapFast(syncMapPtr, sessions)
		}
	}
	
	return false
}

// 快速从Map中提取数据
func extractFromMapFast(mapValue reflect.Value, sessions map[string]SessionItem) bool {
	if mapValue.Len() == 0 {
		return false
	}
	
	count := 0
	for _, key := range mapValue.MapKeys() {
		value := mapValue.MapIndex(key)
		sessionKey := fmt.Sprintf("%v", key.Interface())
		
		if sessionItem, err := processSessionValueFast(sessionKey, value.Interface()); err == nil {
			sessions[sessionKey] = sessionItem
			count++
		}
		
		// 限制处理数量以提高性能
		if count > 1000 {
			break
		}
	}
	
	return count > 0
}

// 快速从sync.Map中提取数据
func extractFromSyncMapFast(syncMapPtr reflect.Value, sessions map[string]SessionItem) bool {
	if syncMap, ok := syncMapPtr.Interface().(*sync.Map); ok {
		count := 0
		syncMap.Range(func(key, value interface{}) bool {
			sessionKey := fmt.Sprintf("%v", key)
			if sessionItem, err := processSessionValueFast(sessionKey, value); err == nil {
				sessions[sessionKey] = sessionItem
				count++
			}
			
			// 限制处理数量以提高性能
			return count < 1000
		})
		return count > 0
	}
	return false
}

// 优化的session值处理
func processSessionValueFast(sessionKey string, sessionValue interface{}) (SessionItem, error) {
	var expiry int64 = time.Now().Add(24 * time.Hour).Unix() // 默认过期时间
	var sessionDataMap map[string]interface{}
	
	// 快速类型判断
	switch data := sessionValue.(type) {
	case []byte:
		if len(data) > 0 {
			if decoded, err := decodeSessionData(data); err == nil {
				sessionDataMap = decoded
			} else {
				// 如果解码失败，尝试直接作为字符串处理
				sessionDataMap = map[string]interface{}{
					"raw_data": string(data),
				}
			}
		} else {
			sessionDataMap = make(map[string]interface{})
		}
		
	case map[string]interface{}:
		sessionDataMap = data
		// 快速查找过期时间
		if exp, exists := data["expiry"]; exists {
			if expTime, ok := exp.(int64); ok {
				expiry = expTime
			} else if expTime, ok := exp.(time.Time); ok {
				expiry = expTime.Unix()
			}
		}
		
	case map[interface{}]interface{}:
		sessionDataMap = convertInterfaceMapToStringMap(data)
		
	default:
		// 简化反射处理
		val := reflect.ValueOf(sessionValue)
		if val.Kind() == reflect.Ptr && !val.IsNil() {
			val = val.Elem()
		}
		
		if val.Kind() == reflect.Struct {
			sessionDataMap = make(map[string]interface{})
			
			// 只查找最常见的字段名
			if dataField := val.FieldByName("Data"); dataField.IsValid() && dataField.CanInterface() {
				if dataMap, ok := dataField.Interface().(map[string]interface{}); ok {
					sessionDataMap = dataMap
				}
			}
			
			// 查找过期时间字段
			if expiryField := val.FieldByName("Expiry"); expiryField.IsValid() && expiryField.CanInterface() {
				if expiryTime, ok := expiryField.Interface().(time.Time); ok {
					expiry = expiryTime.Unix()
				}
			}
		} else {
			// 如果无法处理，创建一个基本的session
			sessionDataMap = map[string]interface{}{
				"session_id": sessionKey,
				"raw_value":  fmt.Sprintf("%v", sessionValue),
			}
		}
	}
	
	return createSessionItemFast(expiry, sessionKey, sessionDataMap), nil
}

// 优化的session item创建
func createSessionItemFast(expiry int64, key string, data map[string]interface{}) SessionItem {
	// 预分配合适大小的map
	sessionItem := make(SessionItem, len(data)+4)
	sessionItem["expiry"] = expiry
	sessionItem["key"] = key
	sessionItem["expiryStr"] = time.Unix(expiry, 0).Format(timeFormat)
	
	// 批量复制数据
	for k, v := range data {
		sessionItem[k] = v
	}
	
	// 确保username字段存在
	if _, exists := sessionItem["username"]; !exists {
		sessionItem["username"] = ""
	}
	
	return sessionItem
}

// extractAllSessionsFromStore 从Fiber v3 session store中提取所有session数据
func extractAllSessionsFromStore(store interface{}) (map[string]SessionItem, error) {
	// 添加调试信息
	fmt.Printf("🔍 开始提取sessions，store类型: %T\n", store)
	
	// 首先尝试快速路径
	if sessions, found := fastExtractSessions(store); found && len(sessions) > 0 {
		fmt.Printf("✅ 快速路径成功，找到 %d 个sessions\n", len(sessions))
		return sessions, nil
	}
	
	fmt.Printf("⚠️ 快速路径未找到数据，尝试深度扫描...\n")
	
	// 回退到深度扫描
	sessions := make(map[string]SessionItem)
	storeValue := reflect.ValueOf(store)
	if storeValue.Kind() == reflect.Ptr {
		storeValue = storeValue.Elem()
	}
	
	// 打印store的结构信息
	storeType := storeValue.Type()
	fmt.Printf("📊 Store结构分析 - 类型: %s, 字段数: %d\n", storeType.Name(), storeValue.NumField())
	
	for i := 0; i < storeValue.NumField(); i++ {
		field := storeValue.Field(i)
		fieldType := storeType.Field(i)
		fmt.Printf("  字段[%d]: %s (类型: %s, 可导出: %v)\n", 
			i, fieldType.Name, fieldType.Type, fieldType.IsExported())
		
		// 如果是Storage字段，进一步分析
		if fieldType.Name == "Storage" && field.IsValid() && !field.IsNil() {
			storage := field.Interface()
			storageValue := reflect.ValueOf(storage)
			if storageValue.Kind() == reflect.Ptr {
				storageValue = storageValue.Elem()
			}
			
			storageType := storageValue.Type()
			fmt.Printf("    🗄️ Storage分析 - 类型: %s, 字段数: %d\n", 
				storageType.Name(), storageValue.NumField())
			
			for j := 0; j < storageValue.NumField(); j++ {
				storageField := storageValue.Field(j)
				storageFieldType := storageType.Field(j)
				fmt.Printf("      Storage字段[%d]: %s (类型: %s, 种类: %s)\n", 
					j, storageFieldType.Name, storageFieldType.Type, storageField.Kind())
				
				// 尝试从这个字段提取数据
				if extractFromFieldWithLogging(storageField, storageFieldType.Name, sessions) {
					fmt.Printf("✅ 从Storage.%s字段成功提取到数据\n", storageFieldType.Name)
				}
			}
		}
	}
	
	fmt.Printf("📈 总共提取到 %d 个sessions\n", len(sessions))
	return sessions, nil
}

// 带日志的字段提取
func extractFromFieldWithLogging(field reflect.Value, fieldName string, sessions map[string]SessionItem) bool {
	fmt.Printf("    🔍 尝试从字段 %s 提取数据...\n", fieldName)
	
	// 处理未导出字段
	if !field.CanInterface() {
		if field.CanAddr() {
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
			fmt.Printf("      📝 使用unsafe访问私有字段\n")
		} else {
			fmt.Printf("      ❌ 无法访问字段\n")
			return false
		}
	}
	
	initialCount := len(sessions)
	
	switch field.Kind() {
	case reflect.Map:
		if field.IsNil() {
			fmt.Printf("      ❌ Map字段为nil\n")
			return false
		}
		mapLen := field.Len()
		fmt.Printf("      📊 Map字段长度: %d\n", mapLen)
		
		if mapLen > 0 {
			// 打印前几个键值作为示例
			keys := field.MapKeys()
			for i, key := range keys {
				if i >= 3 { // 只打印前3个
					fmt.Printf("        ...(还有%d个)\n", mapLen-3)
					break
				}
				value := field.MapIndex(key)
				fmt.Printf("        键[%d]: %v -> %T\n", i, key.Interface(), value.Interface())
			}
			
			extractFromMapFast(field, sessions)
		}
		
	case reflect.Interface, reflect.Ptr:
		if field.IsNil() {
			fmt.Printf("      ❌ 接口/指针字段为nil\n")
			return false
		}
		
		actualType := field.Elem().Type()
		fmt.Printf("      📎 实际类型: %s\n", actualType)
		
		if field.Type().String() == "*sync.Map" || 
		   strings.Contains(field.Type().String(), "sync.Map") {
			fmt.Printf("      🔄 发现sync.Map\n")
			extractFromSyncMapFast(field, sessions)
		} else if field.Elem().Kind() == reflect.Map {
			fmt.Printf("      🗺️ 发现包装的Map\n")
			extractFromMapFast(field.Elem(), sessions)
		}
		
	case reflect.Struct:
		fmt.Printf("      🏗️ 结构体类型: %s\n", field.Type())
		if field.Type().String() == "sync.Map" {
			fmt.Printf("      🔄 发现sync.Map结构体\n")
			syncMapPtr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
			extractFromSyncMapFast(syncMapPtr, sessions)
		}
	
	default:
		fmt.Printf("      ❓ 未处理的字段类型: %s\n", field.Kind())
		return false
	}
	
	extracted := len(sessions) - initialCount
	if extracted > 0 {
		fmt.Printf("      ✅ 成功提取 %d 个sessions\n", extracted)
		return true
	}
	
	fmt.Printf("      ❌ 未提取到数据\n")
	return false
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
		// 预分配slice容量
		allSessionsSlice = make([]SessionItem, 0, len(sessions))
		
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