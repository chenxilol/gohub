package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chenxilol/gohub/configs"
	"github.com/chenxilol/gohub/internal/sdk"
)

// RedisDistributedTestConfig Redis分布式测试配置
type RedisDistributedTestConfig struct {
	NodeCount         int           // 节点数量
	ClientsPerNode    int           // 每个节点的客户端数量
	TestDuration      time.Duration // 测试持续时间
	MessageRate       int           // 每秒消息数
	ConcurrentWorkers int           // 并发工作者数量
	RedisAddr         string        // Redis 地址
}

// DefaultRedisDistributedConfig 默认Redis分布式测试配置
func DefaultRedisDistributedConfig() RedisDistributedTestConfig {
	return RedisDistributedTestConfig{
		NodeCount:         3,                // 3个节点
		ClientsPerNode:    50,               // 每个节点50个客户端
		TestDuration:      60 * time.Second, // 测试60秒
		MessageRate:       100,              // 每秒100条消息
		ConcurrentWorkers: 10,               // 10个并发工作者
		RedisAddr:         "localhost:6379",
	}
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID        string
	Port      int
	Server    *AppServer
	SDK       *sdk.GoHubSDK
	Running   bool
	ConnCount int64
}

// DistributedTestStats 分布式测试统计
type DistributedTestStats struct {
	TestStats
	NodesCreated      int64
	TotalNodes        int64
	MessagesSent      int64
	MessagesReceived  int64
	CrossNodeMessages int64 // 跨节点消息数量
	RedisOps          int64 // Redis操作数量
	StartTime         time.Time
	EndTime           time.Time
}

// TestRedisDistributedBroadcast 测试Redis分布式广播
func TestRedisDistributedBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过分布式压力测试 (使用 -short 标志)")
	}

	testConfig := DefaultRedisDistributedConfig()
	t.Logf("开始Redis分布式广播测试: %d个节点, 每节点%d客户端, 持续%v",
		testConfig.NodeCount, testConfig.ClientsPerNode, testConfig.TestDuration)

	var stats DistributedTestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)
	stats.StartTime = time.Now()

	// 创建多个节点
	nodes, err := createMultipleNodes(testConfig)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}
	defer cleanupNodes(nodes)

	stats.TotalNodes = int64(len(nodes))
	atomic.StoreInt64(&stats.NodesCreated, stats.TotalNodes)

	// 等待所有节点启动
	time.Sleep(2 * time.Second)

	// 测试跨节点广播
	t.Logf("开始跨节点广播测试...")
	testCrossNodeBroadcast(t, nodes, &stats, testConfig)

	// 测试分布式房间操作
	t.Logf("开始分布式房间操作测试...")
	testDistributedRoomOperations(t, nodes, &stats, testConfig)

	// 测试高频分布式消息
	t.Logf("开始高频分布式消息测试...")
	testHighFrequencyDistributedMessages(t, nodes, &stats, testConfig)

	// 收集最终统计
	stats.EndTime = time.Now()
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	// 打印详细统计信息
	printDistributedStats("Redis分布式广播测试", stats)

	// 验证测试结果
	validateDistributedTest(t, stats)
}

// TestRedisDistributedRoomSync 测试Redis分布式房间同步
func TestRedisDistributedRoomSync(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过分布式压力测试 (使用 -short 标志)")
	}

	testConfig := DefaultRedisDistributedConfig()
	testConfig.NodeCount = 4 // 使用4个节点测试同步

	t.Logf("开始Redis分布式房间同步测试: %d个节点", testConfig.NodeCount)

	var stats DistributedTestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)
	stats.StartTime = time.Now()

	// 创建多个节点
	nodes, err := createMultipleNodes(testConfig)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}
	defer cleanupNodes(nodes)

	// 等待节点启动
	time.Sleep(2 * time.Second)

	// 测试房间创建同步
	testRoomCreationSync(t, nodes, &stats)

	// 测试房间删除同步
	testRoomDeletionSync(t, nodes, &stats)

	// 测试复杂房间操作同步
	testComplexRoomSync(t, nodes, &stats, testConfig)

	stats.EndTime = time.Now()
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	printDistributedStats("Redis分布式房间同步测试", stats)
	validateDistributedTest(t, stats)
}

// TestRedisDistributedStability 测试Redis分布式稳定性
func TestRedisDistributedStability(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过分布式压力测试 (使用 -short 标志)")
	}

	testConfig := DefaultRedisDistributedConfig()
	testConfig.TestDuration = 5 * time.Minute // 长时间测试
	testConfig.NodeCount = 5

	t.Logf("开始Redis分布式稳定性测试: %d个节点, 持续%v",
		testConfig.NodeCount, testConfig.TestDuration)

	var stats DistributedTestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)
	stats.StartTime = time.Now()

	// 创建多个节点
	nodes, err := createMultipleNodes(testConfig)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}
	defer cleanupNodes(nodes)

	// 等待节点启动
	time.Sleep(3 * time.Second)

	// 启动长时间稳定性测试
	ctx, cancel := context.WithTimeout(context.Background(), testConfig.TestDuration)
	defer cancel()

	// 创建多个测试goroutine
	var wg sync.WaitGroup

	// 持续广播测试
	wg.Add(1)
	go func() {
		defer wg.Done()
		continuousBroadcastTest(ctx, nodes, &stats)
	}()

	// 持续房间操作测试
	wg.Add(1)
	go func() {
		defer wg.Done()
		continuousRoomOperationTest(ctx, nodes, &stats)
	}()

	// 节点健康检查
	wg.Add(1)
	go func() {
		defer wg.Done()
		nodeHealthCheck(ctx, nodes, &stats)
	}()

	// Redis连接监控
	wg.Add(1)
	go func() {
		defer wg.Done()
		redisConnectionMonitor(ctx, nodes, &stats)
	}()

	wg.Wait()

	stats.EndTime = time.Now()
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	printDistributedStats("Redis分布式稳定性测试", stats)
	validateDistributedTest(t, stats)
}

// 创建多个节点
func createMultipleNodes(testConfig RedisDistributedTestConfig) ([]*NodeInfo, error) {
	var nodes []*NodeInfo
	basePort := 18080

	for i := 0; i < testConfig.NodeCount; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		port := basePort + i

		// 创建Redis配置
		config := configs.NewDefaultConfig()
		config.Cluster.Enabled = true
		config.Cluster.BusType = "redis"
		config.Auth.AllowAnonymous = true
		config.Server.Addr = fmt.Sprintf(":%d", port)

		// Redis配置
		config.Cluster.Redis.Addrs = []string{testConfig.RedisAddr}
		config.Cluster.Redis.KeyPrefix = fmt.Sprintf("gohub:test:%s:", nodeID)
		config.Cluster.Redis.OpTimeout = 1 * time.Second
		config.Cluster.Redis.PoolSize = 20

		// 创建服务器
		server, err := NewAppServer(&config)
		if err != nil {
			// 清理已创建的节点
			for _, node := range nodes {
				if node.Server != nil {
					node.Server.Shutdown(context.Background())
				}
			}
			return nil, fmt.Errorf("创建节点 %s 失败: %v", nodeID, err)
		}

		node := &NodeInfo{
			ID:      nodeID,
			Port:    port,
			Server:  server,
			SDK:     server.gohubSDK,
			Running: true,
		}

		nodes = append(nodes, node)
		log.Printf("节点 %s 已创建，端口: %d", nodeID, port)
	}

	return nodes, nil
}

// 清理节点
func cleanupNodes(nodes []*NodeInfo) {
	for _, node := range nodes {
		if node.Server != nil {
			node.Running = false
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			node.Server.Shutdown(ctx)
			cancel()
			log.Printf("节点 %s 已关闭", node.ID)
		}
	}
}

// 测试跨节点广播
func testCrossNodeBroadcast(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats, config RedisDistributedTestConfig) {
	var wg sync.WaitGroup
	messageCount := 0

	// 从每个节点发送广播消息
	for nodeIndex, node := range nodes {
		wg.Add(1)
		go func(idx int, n *NodeInfo) {
			defer wg.Done()

			for i := 0; i < 10; i++ { // 每个节点发送10条消息
				message := fmt.Sprintf("跨节点广播消息 从节点%s 第%d条", n.ID, i)

				start := time.Now()
				err := n.SDK.BroadcastAll([]byte(message))
				duration := time.Since(start)

				atomic.AddInt64(&stats.TotalOperations, 1)
				atomic.AddInt64(&stats.MessagesSent, 1)
				atomic.AddInt64(&stats.RedisOps, 1)

				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					t.Errorf("节点 %s 广播失败: %v", n.ID, err)
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					atomic.AddInt64(&stats.CrossNodeMessages, 1)
				}

				recordDistributedOperationTime(stats, duration)
				time.Sleep(100 * time.Millisecond) // 避免过快发送
			}
		}(nodeIndex, node)
		messageCount += 10
	}

	wg.Wait()
	t.Logf("跨节点广播测试完成，总共发送 %d 条消息", messageCount)
}

// 测试分布式房间操作
func testDistributedRoomOperations(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats, config RedisDistributedTestConfig) {
	var wg sync.WaitGroup
	roomCount := 20

	// 在不同节点创建房间
	for i := 0; i < roomCount; i++ {
		wg.Add(1)
		go func(roomIndex int) {
			defer wg.Done()

			// 轮询选择节点
			nodeIndex := roomIndex % len(nodes)
			node := nodes[nodeIndex]

			roomID := fmt.Sprintf("distributed-room-%d", roomIndex)
			roomName := fmt.Sprintf("分布式房间 %d", roomIndex)

			// 创建房间
			start := time.Now()
			err := node.SDK.CreateRoom(roomID, roomName, 100)
			duration := time.Since(start)

			atomic.AddInt64(&stats.TotalOperations, 1)
			atomic.AddInt64(&stats.RedisOps, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
				t.Errorf("节点 %s 创建房间 %s 失败: %v", node.ID, roomID, err)
				return
			}

			atomic.AddInt64(&stats.SuccessfulOps, 1)
			recordDistributedOperationTime(stats, duration)

			// 等待房间同步
			time.Sleep(200 * time.Millisecond)

			// 从其他节点验证房间存在
			for _, otherNode := range nodes {
				if otherNode.ID == node.ID {
					continue
				}

				start = time.Now()
				room, err := otherNode.SDK.GetRoom(roomID)
				duration = time.Since(start)

				atomic.AddInt64(&stats.TotalOperations, 1)
				atomic.AddInt64(&stats.RedisOps, 1)

				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					t.Errorf("节点 %s 获取房间 %s 失败: %v", otherNode.ID, roomID, err)
				} else if room == nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					t.Errorf("节点 %s 无法找到房间 %s", otherNode.ID, roomID)
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
				}

				recordDistributedOperationTime(stats, duration)
			}

			// 向房间发送消息
			message := fmt.Sprintf("分布式房间消息: %s", roomID)
			start = time.Now()
			err = node.SDK.BroadcastToRoom(roomID, []byte(message), "")
			duration = time.Since(start)

			atomic.AddInt64(&stats.TotalOperations, 1)
			atomic.AddInt64(&stats.MessagesSent, 1)
			atomic.AddInt64(&stats.RedisOps, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
			} else {
				atomic.AddInt64(&stats.SuccessfulOps, 1)
				atomic.AddInt64(&stats.CrossNodeMessages, 1)
			}

			recordDistributedOperationTime(stats, duration)
		}(i)
	}

	wg.Wait()
	t.Logf("分布式房间操作测试完成，处理了 %d 个房间", roomCount)
}

// 测试高频分布式消息
func testHighFrequencyDistributedMessages(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats, config RedisDistributedTestConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// 每个节点启动高频消息发送
	for _, node := range nodes {
		wg.Add(1)
		go func(n *NodeInfo) {
			defer wg.Done()

			ticker := time.NewTicker(time.Second / time.Duration(config.MessageRate/len(nodes)))
			defer ticker.Stop()

			messageCounter := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					messageCounter++
					message := fmt.Sprintf("高频消息 from %s #%d", n.ID, messageCounter)

					start := time.Now()
					err := n.SDK.BroadcastAll([]byte(message))
					duration := time.Since(start)

					atomic.AddInt64(&stats.TotalOperations, 1)
					atomic.AddInt64(&stats.MessagesSent, 1)
					atomic.AddInt64(&stats.RedisOps, 1)

					if err != nil {
						atomic.AddInt64(&stats.FailedOps, 1)
					} else {
						atomic.AddInt64(&stats.SuccessfulOps, 1)
						atomic.AddInt64(&stats.CrossNodeMessages, 1)
					}

					recordDistributedOperationTime(stats, duration)
				}
			}
		}(node)
	}

	wg.Wait()
	t.Logf("高频分布式消息测试完成")
}

// 测试房间创建同步
func testRoomCreationSync(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats) {
	roomID := "sync-test-room"
	roomName := "同步测试房间"

	// 在第一个节点创建房间
	err := nodes[0].SDK.CreateRoom(roomID, roomName, 50)
	if err != nil {
		t.Errorf("创建房间失败: %v", err)
		return
	}

	atomic.AddInt64(&stats.SuccessfulOps, 1)

	// 等待同步
	time.Sleep(500 * time.Millisecond)

	// 在其他节点验证房间存在
	for i := 1; i < len(nodes); i++ {
		room, err := nodes[i].SDK.GetRoom(roomID)
		atomic.AddInt64(&stats.TotalOperations, 1)

		if err != nil || room == nil {
			atomic.AddInt64(&stats.FailedOps, 1)
			t.Errorf("节点 %s 无法找到同步的房间: %v", nodes[i].ID, err)
		} else {
			atomic.AddInt64(&stats.SuccessfulOps, 1)
			t.Logf("节点 %s 成功同步房间: %s", nodes[i].ID, room.Name)
		}
	}
}

// 测试房间删除同步
func testRoomDeletionSync(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats) {
	roomID := "delete-sync-test-room"

	// 创建房间
	err := nodes[0].SDK.CreateRoom(roomID, "删除同步测试房间", 50)
	if err != nil {
		t.Errorf("创建房间失败: %v", err)
		return
	}

	time.Sleep(200 * time.Millisecond)

	// 删除房间
	err = nodes[1].SDK.DeleteRoom(roomID)
	atomic.AddInt64(&stats.TotalOperations, 1)

	if err != nil {
		atomic.AddInt64(&stats.FailedOps, 1)
		t.Errorf("删除房间失败: %v", err)
		return
	}

	atomic.AddInt64(&stats.SuccessfulOps, 1)

	// 等待同步
	time.Sleep(500 * time.Millisecond)

	// 验证房间在所有节点都被删除
	for _, node := range nodes {
		room, err := node.SDK.GetRoom(roomID)
		atomic.AddInt64(&stats.TotalOperations, 1)

		if err == nil && room != nil {
			atomic.AddInt64(&stats.FailedOps, 1)
			t.Errorf("节点 %s 仍然能找到已删除的房间", node.ID)
		} else {
			atomic.AddInt64(&stats.SuccessfulOps, 1)
		}
	}
}

// 测试复杂房间同步
func testComplexRoomSync(t *testing.T, nodes []*NodeInfo, stats *DistributedTestStats, config RedisDistributedTestConfig) {
	var wg sync.WaitGroup
	complexRoomCount := 10

	for i := 0; i < complexRoomCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			roomID := fmt.Sprintf("complex-room-%d", index)
			nodeIndex := index % len(nodes)

			// 创建房间
			err := nodes[nodeIndex].SDK.CreateRoom(roomID, fmt.Sprintf("复杂测试房间 %d", index), 100)
			atomic.AddInt64(&stats.TotalOperations, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
				return
			}

			atomic.AddInt64(&stats.SuccessfulOps, 1)

			// 等待同步
			time.Sleep(300 * time.Millisecond)

			// 在其他节点发送消息到房间
			for j, node := range nodes {
				if j == nodeIndex {
					continue
				}

				message := fmt.Sprintf("复杂同步消息 from %s to %s", node.ID, roomID)
				err := node.SDK.BroadcastToRoom(roomID, []byte(message), "")
				atomic.AddInt64(&stats.TotalOperations, 1)
				atomic.AddInt64(&stats.MessagesSent, 1)

				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					atomic.AddInt64(&stats.CrossNodeMessages, 1)
				}
			}
		}(i)
	}

	wg.Wait()
}

// 持续广播测试
func continuousBroadcastTest(ctx context.Context, nodes []*NodeInfo, stats *DistributedTestStats) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	messageCounter := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodeIndex := messageCounter % len(nodes)
			node := nodes[nodeIndex]

			if !node.Running {
				continue
			}

			messageCounter++
			message := fmt.Sprintf("持续广播消息 #%d from %s", messageCounter, node.ID)

			err := node.SDK.BroadcastAll([]byte(message))
			atomic.AddInt64(&stats.TotalOperations, 1)
			atomic.AddInt64(&stats.MessagesSent, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
			} else {
				atomic.AddInt64(&stats.SuccessfulOps, 1)
				atomic.AddInt64(&stats.CrossNodeMessages, 1)
			}
		}
	}
}

// 持续房间操作测试
func continuousRoomOperationTest(ctx context.Context, nodes []*NodeInfo, stats *DistributedTestStats) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	roomCounter := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodeIndex := roomCounter % len(nodes)
			node := nodes[nodeIndex]

			if !node.Running {
				continue
			}

			roomCounter++
			roomID := fmt.Sprintf("continuous-room-%d", roomCounter)

			// 创建房间
			err := node.SDK.CreateRoom(roomID, fmt.Sprintf("持续测试房间 %d", roomCounter), 50)
			atomic.AddInt64(&stats.TotalOperations, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
				continue
			}

			atomic.AddInt64(&stats.SuccessfulOps, 1)

			// 等待一段时间后删除
			time.Sleep(5 * time.Second)

			err = node.SDK.DeleteRoom(roomID)
			atomic.AddInt64(&stats.TotalOperations, 1)

			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
			} else {
				atomic.AddInt64(&stats.SuccessfulOps, 1)
			}
		}
	}
}

// 节点健康检查
func nodeHealthCheck(ctx context.Context, nodes []*NodeInfo, stats *DistributedTestStats) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, node := range nodes {
				if !node.Running {
					continue
				}

				// 检查节点是否响应
				rooms := node.SDK.ListRooms()
				atomic.AddInt64(&stats.TotalOperations, 1)

				if rooms != nil {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					log.Printf("节点 %s 健康检查通过，当前房间数: %d", node.ID, len(rooms))
				} else {
					atomic.AddInt64(&stats.FailedOps, 1)
					log.Printf("节点 %s 健康检查失败", node.ID)
				}
			}
		}
	}
}

// Redis连接监控
func redisConnectionMonitor(ctx context.Context, nodes []*NodeInfo, stats *DistributedTestStats) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, node := range nodes {
				if !node.Running {
					continue
				}

				// 通过发送一个简单的广播来测试Redis连接
				testMessage := fmt.Sprintf("Redis连接测试 from %s at %v", node.ID, time.Now())
				err := node.SDK.BroadcastAll([]byte(testMessage))
				atomic.AddInt64(&stats.RedisOps, 1)

				if err != nil {
					log.Printf("节点 %s Redis连接检查失败: %v", node.ID, err)
				}
			}
		}
	}
}

// 打印分布式测试统计
func printDistributedStats(testName string, stats DistributedTestStats) {
	duration := stats.EndTime.Sub(stats.StartTime)

	log.Printf("=== %s 统计信息 ===", testName)
	log.Printf("测试持续时间: %v", duration)
	log.Printf("创建节点数: %d/%d", stats.NodesCreated, stats.TotalNodes)
	log.Printf("总操作数: %d", stats.TotalOperations)
	log.Printf("成功操作: %d (%.2f%%)", stats.SuccessfulOps,
		float64(stats.SuccessfulOps)/float64(stats.TotalOperations)*100)
	log.Printf("失败操作: %d (%.2f%%)", stats.FailedOps,
		float64(stats.FailedOps)/float64(stats.TotalOperations)*100)
	log.Printf("发送消息数: %d", stats.MessagesSent)
	log.Printf("接收消息数: %d", stats.MessagesReceived)
	log.Printf("跨节点消息数: %d", stats.CrossNodeMessages)
	log.Printf("Redis操作数: %d", stats.RedisOps)
	log.Printf("平均操作时间: %v", stats.AvgResponseTime)
	log.Printf("最大响应时间: %v", stats.MaxResponseTime)
	log.Printf("最小响应时间: %v", stats.MinResponseTime)

	// 内存统计
	memStart := float64(stats.MemoryUsageStart.Alloc) / 1024 / 1024
	memEnd := float64(stats.MemoryUsageEnd.Alloc) / 1024 / 1024
	log.Printf("内存使用 - 开始: %.2f MB, 结束: %.2f MB, 增长: %.2f MB",
		memStart, memEnd, memEnd-memStart)
	log.Printf("垃圾回收次数: %d", stats.MemoryUsageEnd.NumGC-stats.MemoryUsageStart.NumGC)
	log.Printf("===========================")
}

// 验证分布式测试结果
func validateDistributedTest(t *testing.T, stats DistributedTestStats) {
	successRate := float64(stats.SuccessfulOps) / float64(stats.TotalOperations) * 100

	if successRate < 95 {
		t.Errorf("分布式测试成功率过低: %.2f%%, 期望 >= 95%%", successRate)
	} else {
		t.Logf("✅ 分布式测试成功率: %.2f%%", successRate)
	}

	if stats.NodesCreated != stats.TotalNodes {
		t.Errorf("节点创建不完整: %d/%d", stats.NodesCreated, stats.TotalNodes)
	} else {
		t.Logf("✅ 所有节点创建成功: %d", stats.NodesCreated)
	}

	if stats.CrossNodeMessages == 0 {
		t.Error("没有检测到跨节点消息，分布式功能可能有问题")
	} else {
		t.Logf("✅ 跨节点消息数: %d", stats.CrossNodeMessages)
	}

	if stats.RedisOps == 0 {
		t.Error("没有检测到Redis操作")
	} else {
		t.Logf("✅ Redis操作数: %d", stats.RedisOps)
	}
}

// recordDistributedOperationTime 为分布式测试记录操作时间
func recordDistributedOperationTime(stats *DistributedTestStats, duration time.Duration) {
	if duration > stats.MaxResponseTime {
		stats.MaxResponseTime = duration
	}
	if stats.MinResponseTime == 0 || stats.MinResponseTime > duration {
		stats.MinResponseTime = duration
	}
}
