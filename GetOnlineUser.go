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

// extractSessionsFromConfig 从Config中提取storage信息
func extractSessionsFromConfig(config interface{}) (map[string]SessionItem, error) {
	sessions := make(map[string]SessionItem)
	
	configValue := reflect.ValueOf(config)
	if configValue.Kind() == reflect.Ptr {
		configValue = configValue.Elem()
	}
	
	configType := configValue.Type()
	fmt.Printf("    🔧 Config分析 - 类型: %s, 字段数: %d\n", configType.Name(), configValue.NumField())
	
	for i := 0; i < configValue.NumField(); i++ {
		field := configValue.Field(i)
		fieldType := configType.Field(i)
		
		fmt.Printf("      Config字段[%d]: %s (类型: %s, 种类: %s)\n", 
			i, fieldType.Name, fieldType.Type, field.Kind())
		
		// 查找Storage字段
		if fieldType.Name == "Storage" && field.IsValid() && !field.IsNil() {
			fmt.Printf("      ✅ 在Config中找到Storage字段\n")
			storage := field.Interface()
			return extractFromStorage(storage)
		}
	}
	
	return sessions, fmt.Errorf("在Config中未找到Storage字段")
}

// extractFromStorage 从storage中提取sessions
func extractFromStorage(storage interface{}) (map[string]SessionItem, error) {
	sessions := make(map[string]SessionItem)
	
	storageValue := reflect.ValueOf(storage)
	if storageValue.Kind() == reflect.Ptr {
		storageValue = storageValue.Elem()
	}
	
	storageType := storageValue.Type()
	fmt.Printf("      🗄️ Storage详细分析 - 类型: %s, 字段数: %d\n", 
		storageType.Name(), storageValue.NumField())
	
	// 遍历storage的所有字段
	for i := 0; i < storageValue.NumField(); i++ {
		field := storageValue.Field(i)
		fieldType := storageType.Field(i)
		
		// 处理未导出字段
		if !field.CanInterface() {
			if field.CanAddr() {
				field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
			} else {
				fmt.Printf("        字段[%d]: %s - 无法访问\n", i, fieldType.Name)
				continue
			}
		}
		
		fmt.Printf("        Storage字段[%d]: %s (类型: %s, 种类: %s)\n", 
			i, fieldType.Name, fieldType.Type, field.Kind())
		
		// 查找可能存储session数据的字段
		fieldName := strings.ToLower(fieldType.Name)
		if strings.Contains(fieldName, "data") || 
		   strings.Contains(fieldName, "store") || 
		   strings.Contains(fieldName, "session") ||
		   strings.Contains(fieldName, "map") ||
		   strings.Contains(fieldName, "cache") ||
		   fieldName == "db" {
			
			fmt.Printf("        🎯 检测到可能的session存储字段: %s\n", fieldType.Name)
			
			if extractFromFieldDetailed(field, fieldType.Name, sessions) {
				fmt.Printf("        ✅ 从%s字段成功提取到数据\n", fieldType.Name)
				return sessions, nil
			}
		}
	}
	
	return sessions, fmt.Errorf("未在storage中找到session数据")
}

// extractFromFieldDetailed 详细的字段提取，带调试信息
func extractFromFieldDetailed(field reflect.Value, fieldName string, sessions map[string]SessionItem) bool {
	fmt.Printf("          🔍 详细分析字段: %s\n", fieldName)
	
	switch field.Kind() {
	case reflect.Map:
		if field.IsNil() {
			fmt.Printf("            ❌ Map为nil\n")
			return false
		}
		
		mapLen := field.Len()
		fmt.Printf("            📊 Map长度: %d\n", mapLen)
		
		if mapLen > 0 {
			// 打印所有键值对作为调试
			keys := field.MapKeys()
			for i, key := range keys {
				value := field.MapIndex(key)
				keyStr := fmt.Sprintf("%v", key.Interface())
				valueType := value.Type()
				fmt.Printf("              键[%d]: %s -> %s\n", i, keyStr, valueType)
				
				// 尝试处理这个session
				if sessionItem, err := processSessionValueDetailed(keyStr, value.Interface()); err == nil {
					sessions[keyStr] = sessionItem
					fmt.Printf("              ✅ 成功处理session: %s\n", keyStr)
				} else {
					fmt.Printf("              ❌ 处理session失败: %s, 错误: %v\n", keyStr, err)
				}
			}
			return len(sessions) > 0
		}
		
	case reflect.Interface, reflect.Ptr:
		if field.IsNil() {
			fmt.Printf("            ❌ 接口/指针为nil\n")
			return false
		}
		
		actualValue := field.Elem()
		actualType := actualValue.Type()
		fmt.Printf("            📎 实际类型: %s, 种类: %s\n", actualType, actualValue.Kind())
		
		if actualValue.Kind() == reflect.Map {
			fmt.Printf("            🗺️ 发现嵌套Map\n")
			return extractFromFieldDetailed(actualValue, fieldName+"_inner", sessions)
		}
		
		// 检查sync.Map
		if strings.Contains(field.Type().String(), "sync.Map") {
			fmt.Printf("            🔄 发现sync.Map\n")
			return extractFromSyncMapDetailed(field, sessions)
		}
		
	case reflect.Struct:
		fmt.Printf("            🏗️ 结构体: %s\n", field.Type())
		
		if field.Type().String() == "sync.Map" {
			fmt.Printf("            🔄 发现sync.Map结构体\n")
			syncMapPtr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
			return extractFromSyncMapDetailed(syncMapPtr, sessions)
		}
		
		// 递归分析结构体字段
		for i := 0; i < field.NumField(); i++ {
			subField := field.Field(i)
			subFieldType := field.Type().Field(i)
			
			fmt.Printf("            子字段[%d]: %s (类型: %s)\n", 
				i, subFieldType.Name, subFieldType.Type)
			
			if extractFromFieldDetailed(subField, fieldName+"_"+subFieldType.Name, sessions) {
				return true
			}
		}
	
	default:
		fmt.Printf("            ❓ 未处理的类型: %s\n", field.Kind())
	}
	
	return false
}

// extractFromSyncMapDetailed 详细的sync.Map提取
func extractFromSyncMapDetailed(syncMapPtr reflect.Value, sessions map[string]SessionItem) bool {
	if syncMap, ok := syncMapPtr.Interface().(*sync.Map); ok {
		count := 0
		syncMap.Range(func(key, value interface{}) bool {
			keyStr := fmt.Sprintf("%v", key)
			fmt.Printf("              sync.Map键: %s -> %T\n", keyStr, value)
			
			if sessionItem, err := processSessionValueDetailed(keyStr, value); err == nil {
				sessions[keyStr] = sessionItem
				count++
				fmt.Printf("              ✅ 成功处理sync.Map session: %s\n", keyStr)
			} else {
				fmt.Printf("              ❌ 处理sync.Map session失败: %s, 错误: %v\n", keyStr, err)
			}
			
			return count < 100 // 限制数量
		})
		return count > 0
	}
	return false
}

// processSessionValueDetailed 详细的session值处理
func processSessionValueDetailed(sessionKey string, sessionValue interface{}) (SessionItem, error) {
	fmt.Printf("                🔍 处理session值: %s, 类型: %T\n", sessionKey, sessionValue)
	
	var expiry int64 = time.Now().Add(24 * time.Hour).Unix() // 默认过期时间
	var sessionDataMap map[string]interface{}
	
	// 详细类型判断
	switch data := sessionValue.(type) {
	case []byte:
		fmt.Printf("                📦 字节数组长度: %d\n", len(data))
		if len(data) > 0 {
			if decoded, err := decodeSessionData(data); err == nil {
				sessionDataMap = decoded
				fmt.Printf("                ✅ gob解码成功\n")
			} else {
				fmt.Printf("                ⚠️ gob解码失败，作为字符串处理: %v\n", err)
				sessionDataMap = map[string]interface{}{
					"raw_data": string(data),
				}
			}
		} else {
			sessionDataMap = make(map[string]interface{})
		}
		
	case map[string]interface{}:
		fmt.Printf("                🗺️ 直接的string map，字段数: %d\n", len(data))
		sessionDataMap = data
		
		// 查找过期时间
		if exp, exists := data["expiry"]; exists {
			fmt.Printf("                ⏰ 找到expiry字段: %v\n", exp)
			if expTime, ok := exp.(int64); ok {
				expiry = expTime
			} else if expTime, ok := exp.(time.Time); ok {
				expiry = expTime.Unix()
			}
		}
		
	case map[interface{}]interface{}:
		fmt.Printf("                🗺️ interface map，字段数: %d\n", len(data))
		sessionDataMap = convertInterfaceMapToStringMap(data)
		
	default:
		fmt.Printf("                🔍 其他类型，尝试反射处理\n")
		val := reflect.ValueOf(sessionValue)
		if val.Kind() == reflect.Ptr && !val.IsNil() {
			val = val.Elem()
		}
		
		if val.Kind() == reflect.Struct {
			sessionDataMap = make(map[string]interface{})
			structType := val.Type()
			fmt.Printf("                🏗️ 结构体字段数: %d\n", val.NumField())
			
			// 查找Data字段
			if dataField := val.FieldByName("Data"); dataField.IsValid() && dataField.CanInterface() {
				fmt.Printf("                📊 找到Data字段\n")
				if dataMap, ok := dataField.Interface().(map[string]interface{}); ok {
					sessionDataMap = dataMap
				}
			}
			
			// 查找所有字段并打印
			for i := 0; i < val.NumField(); i++ {
				field := val.Field(i)
				fieldType := structType.Field(i)
				if field.CanInterface() {
					fmt.Printf("                  结构体字段[%d]: %s = %v\n", 
						i, fieldType.Name, field.Interface())
					
					// 将字段添加到sessionDataMap
					sessionDataMap[strings.ToLower(fieldType.Name)] = field.Interface()
				}
			}
		} else {
			// 创建基本session
			sessionDataMap = map[string]interface{}{
				"session_id": sessionKey,
				"raw_value":  fmt.Sprintf("%v", sessionValue),
			}
		}
	}
	
	sessionItem := createSessionItemFast(expiry, sessionKey, sessionDataMap)
	fmt.Printf("                ✅ 创建session item成功，字段数: %d\n", len(sessionItem))
	return sessionItem, nil
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
		
		// 处理Config字段
		if fieldType.Name == "Config" && field.IsValid() {
			fmt.Printf("  🔧 分析Config字段...\n")
			config := field.Interface()
			if configSessions, err := extractSessionsFromConfig(config); err == nil && len(configSessions) > 0 {
				fmt.Printf("  ✅ 从Config中成功提取到 %d 个sessions\n", len(configSessions))
				return configSessions, nil
			} else {
				fmt.Printf("  ⚠️ 从Config中提取失败: %v\n", err)
			}
		}
		
		// 尝试直接从store字段中查找session数据
		fieldName := strings.ToLower(fieldType.Name)
		if strings.Contains(fieldName, "session") || 
		   strings.Contains(fieldName, "data") || 
		   strings.Contains(fieldName, "store") ||
		   strings.Contains(fieldName, "registry") {
			
			fmt.Printf("  🎯 检测到可能的session字段: %s\n", fieldType.Name)
			
			if extractFromFieldDetailed(field, fieldType.Name, sessions) {
				fmt.Printf("  ✅ 从Store.%s字段成功提取到数据\n", fieldType.Name)
				return sessions, nil
			}
		}
	}
	
	// 如果还是没找到，尝试查找所有私有字段
	fmt.Printf("🔍 尝试访问私有字段...\n")
	for i := 0; i < storeValue.NumField(); i++ {
		field := storeValue.Field(i)
		fieldType := storeType.Field(i)
		
		if !fieldType.IsExported() {
			fmt.Printf("  🔒 私有字段: %s\n", fieldType.Name)
			
			if field.CanAddr() {
				privateField := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
				if extractFromFieldDetailed(privateField, fieldType.Name, sessions) {
					fmt.Printf("  ✅ 从私有字段%s成功提取到数据\n", fieldType.Name)
					return sessions, nil
				}
			}
		}
	}
	
	fmt.Printf("📈 总共提取到 %d 个sessions\n", len(sessions))
	return sessions, nil
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