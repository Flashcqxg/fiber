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

func GetOnlineUser(c CtxHelper) error {

	sess, err := US.Get(c)
	if err != nil {
		return c.Fail(err.Error())
	}
	defer sess.Release()
	fmt.Printf("当前ID为：%s \n", sess.ID())

	rows, err := Storage.Conn().Query("select e,k,v from sessions")
	if err != nil {
		return c.Fail(err.Error())
	}
	defer rows.Close()

	// 获取查询参数 - 保留原始大小写用于展示，但过滤时使用小写
	rawUsernameFilter := c.Ctx.Query("username")
	usernameFilter := strings.ToLower(rawUsernameFilter)

	page := fiber.Query[int](c, "page", 1)
	limit := fiber.Query[int](c, "limit", 10)
	sortField := fiber.Query[string](c, "sortField", "expiry") // 排序字段: username 或 expiry
	sortOrder := fiber.Query[string](c, "sortOrder", "asc")    // 排序方向: asc 或 desc

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	// 处理结果集
	var allSessions []map[string]interface{}
	for rows.Next() {
		var e int64
		var k string
		var v []byte
		if err := rows.Scan(&e, &k, &v); err != nil {
			log.Error("错误_Scan：", err.Error())
			continue
		}
		fmt.Printf("v：%s \n", v)
		
		// 修复：使用正确的类型进行解码
		rawData := make(map[interface{}]interface{})
		buf := bytes.NewBuffer(v)
		dec := gob.NewDecoder(buf)
		if err := dec.Decode(&rawData); err != nil {
			log.Error("错误_Decode：", err.Error())
			continue
		}
		
		// 将 map[interface{}]interface{} 转换为 map[string]interface{}
		data := make(map[string]interface{})
		for key, value := range rawData {
			if strKey, ok := key.(string); ok {
				data[strKey] = value
			} else {
				// 如果key不是string类型，尝试转换为string
				data[fmt.Sprintf("%v", key)] = value
			}
		}
		
		//要转化为北京时间
		//tz, err := time.LoadLocation("Asia/Shanghai")
		//if err != nil {
		//	log.Error("错误_time.LoadLocation：", err.Error())
		//}
		// 创建会话项并平铺所有字段
		sessionItem := map[string]interface{}{
			"expiry":    e,
			"key":       k,
			"expiryStr": time.Unix(e, 0).Format(time.RFC3339),
			//"expiryStr": time.Unix(e, 0).In(tz).Format(time.RFC3339),
		}
		// 将data中的所有字段合并到外层
		for key, value := range data {
			sessionItem[key] = value
		}
		// 确保username字段存在（用于过滤和排序）
		if _, exists := sessionItem["username"]; !exists {
			sessionItem["username"] = ""
		}
		// 获取用户名并转换为小写
		username, ok := sessionItem["username"].(string)
		if !ok {
			username = ""
		}
		fmt.Printf("当前：%v\n", username)
		usernameLower := strings.ToLower(username)
		// 关键安全规则：永远不返回 admin 用户（不区分大小写）
		//if usernameLower == "admin" {
		//	continue
		//}
		// 用户名过滤 - 不区分大小写
		if rawUsernameFilter != "" {
			if !strings.Contains(usernameLower, usernameFilter) {
				continue
			}
		}
		allSessions = append(allSessions, sessionItem)
	}
	// 根据排序参数进行排序
	sort.Slice(allSessions, func(i, j int) bool {
		switch sortField {
		case "expiry":
			expiryI, _ := allSessions[i]["expiry"].(int64)
			expiryJ, _ := allSessions[j]["expiry"].(int64)

			if sortOrder == "asc" {
				return expiryI < expiryJ
			}
			return expiryI > expiryJ

		default: // 默认按username排序
			userI, ok1 := allSessions[i]["username"].(string)
			userJ, ok2 := allSessions[j]["username"].(string)
			// 处理空用户名的情况
			if !ok1 || userI == "" {
				userI = "zzzzzzzz" // 空用户名排最后
			}
			if !ok2 || userJ == "" {
				userJ = "zzzzzzzz"
			}
			// 使用小写进行比较，但保留原始排序值
			userILower := strings.ToLower(userI)
			userJLower := strings.ToLower(userJ)
			if sortOrder == "asc" {
				return userILower < userJLower
			}
			return userILower > userJLower
		}
	})
	total := len(allSessions)
	// 分页处理
	start := (page - 1) * limit
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	// 确保始终返回数组而非nil
	var pagedSessions []map[string]interface{}
	if total > 0 {
		pagedSessions = allSessions[start:end]
	} else {
		pagedSessions = []map[string]interface{}{} // 显式空数组
	}
	// 返回结果
	return c.JSON(fiber.Map{
		"code": 0,
		"message": fiber.Map{
			"list":  pagedSessions,
			"count": total,
			"times": 0,
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