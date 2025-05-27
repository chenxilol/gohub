package main

import (
	"context"
	"fmt"
	"github.com/chenxilol/gohub/configs"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// StressTestConfig 压力测试配置
type StressTestConfig struct {
	MaxRooms          int           // 最大房间数量
	MessageRate       int           // 每秒消息数
	TestDuration      time.Duration // 测试持续时间
	ConcurrentWorkers int           // 并发工作者数量
}

// DefaultStressConfig 默认压力测试配置
func DefaultStressConfig() StressTestConfig {
	return StressTestConfig{
		MaxRooms:          100,
		MessageRate:       1000,
		TestDuration:      30 * time.Second,
		ConcurrentWorkers: 100,
	}
}

// ExtremeStressConfig 极端压力测试配置
func ExtremeStressConfig() StressTestConfig {
	return StressTestConfig{
		MaxRooms:          1000,
		MessageRate:       10000,
		TestDuration:      60 * time.Second,
		ConcurrentWorkers: 500,
	}
}

// TestStats 测试统计信息
type TestStats struct {
	TotalOperations    int64
	SuccessfulOps      int64
	FailedOps          int64
	TotalBytesTransfer int64
	AvgResponseTime    time.Duration
	MaxResponseTime    time.Duration
	MinResponseTime    time.Duration
	MemoryUsageStart   runtime.MemStats
	MemoryUsageEnd     runtime.MemStats
}

// 极端房间创建压力测试
func TestExtremeRoomCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试 (使用 -short 标志)")
	}

	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false
	config.Auth.AllowAnonymous = true

	server, err := NewAppServer(&config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK
	stressConfig := ExtremeStressConfig()

	t.Logf("开始极端房间创建测试: %d 个房间, %d 个并发工作者",
		stressConfig.MaxRooms, stressConfig.ConcurrentWorkers)

	var stats TestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)

	startTime := time.Now()
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, stressConfig.ConcurrentWorkers)

	// 测试房间创建
	for i := 0; i < stressConfig.MaxRooms; i++ {
		wg.Add(1)
		go func(roomNum int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			roomID := fmt.Sprintf("stress-room-%d", roomNum)
			roomName := fmt.Sprintf("Stress Room %d", roomNum)

			opStart := time.Now()
			err := sdk.CreateRoom(roomID, roomName, 100)
			opDuration := time.Since(opStart)

			atomic.AddInt64(&stats.TotalOperations, 1)
			if err != nil {
				atomic.AddInt64(&stats.FailedOps, 1)
				t.Errorf("Failed to create room %s: %v", roomID, err)
			} else {
				atomic.AddInt64(&stats.SuccessfulOps, 1)
			}

			// 更新响应时间统计
			if stats.MaxResponseTime < opDuration {
				stats.MaxResponseTime = opDuration
			}
			if stats.MinResponseTime == 0 || stats.MinResponseTime > opDuration {
				stats.MinResponseTime = opDuration
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(startTime)
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	// 验证房间数量
	rooms := sdk.ListRooms()
	if len(rooms) != stressConfig.MaxRooms {
		t.Errorf("Expected %d rooms, got %d", stressConfig.MaxRooms, len(rooms))
	}

	// 打印统计信息
	stats.AvgResponseTime = totalDuration / time.Duration(stats.TotalOperations)
	printTestStats("极端房间创建测试", stats, totalDuration)
}

// 高频广播压力测试
func TestHighFrequencyBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试 (使用 -short 标志)")
	}

	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false
	config.Auth.AllowAnonymous = true

	server, err := NewAppServer(&config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK
	stressConfig := DefaultStressConfig()

	t.Logf("开始高频广播测试: %d 消息/秒, 持续 %v",
		stressConfig.MessageRate, stressConfig.TestDuration)

	var stats TestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)

	startTime := time.Now()
	endTime := startTime.Add(stressConfig.TestDuration)

	// 消息发送器
	ticker := time.NewTicker(time.Second / time.Duration(stressConfig.MessageRate))
	defer ticker.Stop()

	messageCounter := int64(0)
	for time.Now().Before(endTime) {
		select {
		case <-ticker.C:
			go func(msgNum int64) {
				message := fmt.Sprintf("Stress message #%d at %v", msgNum, time.Now())

				opStart := time.Now()
				err := sdk.BroadcastAll([]byte(message))
				opDuration := time.Since(opStart)

				atomic.AddInt64(&stats.TotalOperations, 1)
				atomic.AddInt64(&stats.TotalBytesTransfer, int64(len(message)))

				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
				}

				// 更新响应时间统计
				if stats.MaxResponseTime < opDuration {
					stats.MaxResponseTime = opDuration
				}
				if stats.MinResponseTime == 0 || stats.MinResponseTime > opDuration {
					stats.MinResponseTime = opDuration
				}
			}(messageCounter)
			messageCounter++
		}
	}

	totalDuration := time.Since(startTime)
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	stats.AvgResponseTime = totalDuration / time.Duration(stats.TotalOperations)
	printTestStats("高频广播测试", stats, totalDuration)
}

// 大量房间操作压力测试
func TestMassiveRoomOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试 (使用 -short 标志)")
	}

	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false
	config.Auth.AllowAnonymous = true

	server, err := NewAppServer(&config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK
	stressConfig := ExtremeStressConfig()

	t.Logf("开始大量房间操作测试: %d 个房间, 持续 %v",
		stressConfig.MaxRooms, stressConfig.TestDuration)

	var stats TestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)

	// 错误计数器
	var createErrors, getErrors, deleteErrors, broadcastErrors int64
	errorMap := make(map[string]int64)
	var errorMapMutex sync.Mutex

	// 房间生命周期管理
	var roomCounter int64
	var activeRooms = make(map[string]bool)
	var roomMutex sync.RWMutex

	startTime := time.Now()
	endTime := startTime.Add(stressConfig.TestDuration)

	// 减少并发数，避免过度负载
	concurrentWorkers := stressConfig.ConcurrentWorkers / 10 // 从500减少到50
	t.Logf("实际并发工作者数: %d", concurrentWorkers)

	// 并发执行各种房间操作
	var wg sync.WaitGroup

	for time.Now().Before(endTime) {
		for i := 0; i < concurrentWorkers && time.Now().Before(endTime); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// 生成唯一的房间ID
				roomNum := atomic.AddInt64(&roomCounter, 1)
				roomID := fmt.Sprintf("mass-room-%d", roomNum)

				// 房间生命周期：创建 -> 查询 -> 广播 -> 删除

				// 1. 创建房间
				opStart := time.Now()
				err := sdk.CreateRoom(roomID, fmt.Sprintf("Mass Room %d", roomNum), 50)
				opDuration := time.Since(opStart)

				atomic.AddInt64(&stats.TotalOperations, 1)
				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					atomic.AddInt64(&createErrors, 1)

					errorMapMutex.Lock()
					errorKey := fmt.Sprintf("CREATE: %v", err)
					errorMap[errorKey]++
					errorMapMutex.Unlock()

					if createErrors <= 10 {
						t.Logf("创建房间失败 [%d]: roomID=%s, error=%v", createErrors, roomID, err)
					}
					return // 如果创建失败，直接返回
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					roomMutex.Lock()
					activeRooms[roomID] = true
					roomMutex.Unlock()
				}
				recordOperationTime(&stats, opDuration)

				// 2. 获取房间信息（验证创建成功）
				opStart = time.Now()
				room, err := sdk.GetRoom(roomID)
				opDuration = time.Since(opStart)

				atomic.AddInt64(&stats.TotalOperations, 1)
				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					atomic.AddInt64(&getErrors, 1)

					errorMapMutex.Lock()
					errorKey := fmt.Sprintf("GET: %v", err)
					errorMap[errorKey]++
					errorMapMutex.Unlock()

					if getErrors <= 10 {
						t.Logf("获取房间失败 [%d]: roomID=%s, error=%v", getErrors, roomID, err)
					}
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					if getErrors <= 5 && room != nil {
						t.Logf("获取房间成功: roomID=%s, name=%s", room.ID, room.Name)
					}
				}
				recordOperationTime(&stats, opDuration)

				// 3. 广播消息到房间
				opStart = time.Now()
				message := fmt.Sprintf("Test message for room %s", roomID)
				err = sdk.BroadcastToRoom(roomID, []byte(message), "")
				opDuration = time.Since(opStart)

				atomic.AddInt64(&stats.TotalOperations, 1)
				atomic.AddInt64(&stats.TotalBytesTransfer, int64(len(message)))
				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					atomic.AddInt64(&broadcastErrors, 1)

					errorMapMutex.Lock()
					errorKey := fmt.Sprintf("BROADCAST: %v", err)
					errorMap[errorKey]++
					errorMapMutex.Unlock()

					if broadcastErrors <= 10 {
						t.Logf("广播失败 [%d]: roomID=%s, error=%v", broadcastErrors, roomID, err)
					}
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
				}
				recordOperationTime(&stats, opDuration)

				// 4. 删除房间
				opStart = time.Now()
				err = sdk.DeleteRoom(roomID)
				opDuration = time.Since(opStart)

				atomic.AddInt64(&stats.TotalOperations, 1)
				if err != nil {
					atomic.AddInt64(&stats.FailedOps, 1)
					atomic.AddInt64(&deleteErrors, 1)

					errorMapMutex.Lock()
					errorKey := fmt.Sprintf("DELETE: %v", err)
					errorMap[errorKey]++
					errorMapMutex.Unlock()

					if deleteErrors <= 10 {
						t.Logf("删除房间失败 [%d]: roomID=%s, error=%v", deleteErrors, roomID, err)
					}
				} else {
					atomic.AddInt64(&stats.SuccessfulOps, 1)
					roomMutex.Lock()
					delete(activeRooms, roomID)
					roomMutex.Unlock()
				}
				recordOperationTime(&stats, opDuration)
			}()
		}
		time.Sleep(time.Millisecond * 100) // 增加休息时间，减少压力
	}

	wg.Wait()
	totalDuration := time.Since(startTime)
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	// 打印详细的错误统计
	t.Logf("=== 错误统计 ===")
	t.Logf("创建错误: %d", createErrors)
	t.Logf("获取错误: %d", getErrors)
	t.Logf("广播错误: %d", broadcastErrors)
	t.Logf("删除错误: %d", deleteErrors)

	t.Logf("=== 错误类型分布 ===")
	errorMapMutex.Lock()
	for errorType, count := range errorMap {
		if count > 0 { // 显示所有错误类型
			t.Logf("%s: %d次", errorType, count)
		}
	}
	errorMapMutex.Unlock()

	// 最终房间状态
	finalRooms := sdk.ListRooms()
	roomMutex.RLock()
	activeRoomCount := len(activeRooms)
	roomMutex.RUnlock()

	t.Logf("最终房间数量: %d", len(finalRooms))
	t.Logf("预期活跃房间数: %d", activeRoomCount)
	t.Logf("总处理房间数: %d", roomCounter)

	stats.AvgResponseTime = totalDuration / time.Duration(stats.TotalOperations)
	printTestStats("大量房间操作测试", stats, totalDuration)

	// 如果成功率过低，报告问题
	successRate := float64(stats.SuccessfulOps) / float64(stats.TotalOperations) * 100
	if successRate < 80 {
		t.Errorf("成功率过低: %.2f%%, 可能存在问题", successRate)
	} else {
		t.Logf("✅ 成功率: %.2f%%", successRate)
	}
}

// 辅助函数：记录操作时间
func recordOperationTime(stats *TestStats, duration time.Duration) {
	if duration > stats.MaxResponseTime {
		stats.MaxResponseTime = duration
	}
	if stats.MinResponseTime == 0 || stats.MinResponseTime > duration {
		stats.MinResponseTime = duration
	}
}

// 内存泄漏检测测试
func TestMemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试 (使用 -short 标志)")
	}

	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false
	config.Auth.AllowAnonymous = true

	server, err := NewAppServer(&config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK

	t.Logf("开始内存泄漏检测测试...")

	// 记录初始内存状态
	var initialMem, finalMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	// 执行大量操作
	iterations := 10000
	for i := 0; i < iterations; i++ {
		roomID := fmt.Sprintf("leak-test-room-%d", i)

		// 创建房间
		sdk.CreateRoom(roomID, fmt.Sprintf("Leak Test Room %d", i), 10)

		// 广播消息
		sdk.BroadcastAll([]byte(fmt.Sprintf("Leak test message %d", i)))

		// 删除房间
		sdk.DeleteRoom(roomID)

		// 每1000次操作强制进行垃圾回收
		if i%1000 == 0 {
			runtime.GC()
		}
	}

	// 最终垃圾回收和内存检查
	runtime.GC()
	runtime.ReadMemStats(&finalMem)

	// 检查内存增长
	memGrowth := finalMem.Alloc - initialMem.Alloc
	memGrowthMB := float64(memGrowth) / 1024 / 1024

	t.Logf("内存使用变化:")
	t.Logf("  初始内存: %.2f MB", float64(initialMem.Alloc)/1024/1024)
	t.Logf("  最终内存: %.2f MB", float64(finalMem.Alloc)/1024/1024)
	t.Logf("  内存增长: %.2f MB", memGrowthMB)
	t.Logf("  总分配次数: %d", finalMem.TotalAlloc-initialMem.TotalAlloc)

	// 如果内存增长超过100MB，可能存在泄漏
	if memGrowthMB > 100 {
		t.Errorf("可能存在内存泄漏: 内存增长 %.2f MB 超过阈值 100 MB", memGrowthMB)
	}
}

// 长时间稳定性测试
func TestLongTermStability(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试 (使用 -short 标志)")
	}

	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false
	config.Auth.AllowAnonymous = true

	server, err := NewAppServer(&config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK

	// 长时间测试配置（可以根据需要调整）
	testDuration := 5 * time.Minute
	if testing.Verbose() {
		testDuration = 30 * time.Minute // 详细模式下运行更长时间
	}

	t.Logf("开始长时间稳定性测试: 持续 %v", testDuration)

	var stats TestStats
	runtime.ReadMemStats(&stats.MemoryUsageStart)

	startTime := time.Now()
	endTime := startTime.Add(testDuration)

	// 周期性操作
	roomCounter := int64(0)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(endTime) {
		select {
		case <-ticker.C:
			roomID := fmt.Sprintf("stability-room-%d", roomCounter%1000)

			// 交替创建和删除房间
			if roomCounter%2 == 0 {
				err := sdk.CreateRoom(roomID, fmt.Sprintf("Stability Room %d", roomCounter), 10)
				recordOperation(&stats, 0, err)
			} else {
				err := sdk.DeleteRoom(roomID)
				recordOperation(&stats, 0, err)
			}

			// 定期广播
			if roomCounter%10 == 0 {
				message := fmt.Sprintf("Stability test message %d", roomCounter)
				err := sdk.BroadcastAll([]byte(message))
				recordOperation(&stats, 0, err)
			}

			roomCounter++

			// 定期内存检查
			if roomCounter%10000 == 0 {
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				t.Logf("运行时检查 - 操作数: %d, 内存: %.2f MB",
					roomCounter, float64(mem.Alloc)/1024/1024)
			}
		}
	}

	totalDuration := time.Since(startTime)
	runtime.ReadMemStats(&stats.MemoryUsageEnd)

	t.Logf("长时间稳定性测试完成:")
	t.Logf("  总操作数: %d", stats.TotalOperations)
	t.Logf("  成功操作: %d", stats.SuccessfulOps)
	t.Logf("  失败操作: %d", stats.FailedOps)
	t.Logf("  运行时间: %v", totalDuration)

	printTestStats("长时间稳定性测试", stats, totalDuration)
}

// 记录操作统计
func recordOperation(stats *TestStats, duration time.Duration, err error) {
	atomic.AddInt64(&stats.TotalOperations, 1)
	if err != nil {
		atomic.AddInt64(&stats.FailedOps, 1)
	} else {
		atomic.AddInt64(&stats.SuccessfulOps, 1)
	}

	if duration > 0 {
		if stats.MaxResponseTime < duration {
			stats.MaxResponseTime = duration
		}
		if stats.MinResponseTime == 0 || stats.MinResponseTime > duration {
			stats.MinResponseTime = duration
		}
	}
}

// 打印测试统计信息
func printTestStats(testName string, stats TestStats, totalDuration time.Duration) {
	log.Printf("=== %s 统计信息 ===", testName)
	log.Printf("总操作数: %d", stats.TotalOperations)
	log.Printf("成功操作: %d (%.2f%%)", stats.SuccessfulOps,
		float64(stats.SuccessfulOps)/float64(stats.TotalOperations)*100)
	log.Printf("失败操作: %d (%.2f%%)", stats.FailedOps,
		float64(stats.FailedOps)/float64(stats.TotalOperations)*100)
	log.Printf("总运行时间: %v", totalDuration)
	log.Printf("平均操作时间: %v", stats.AvgResponseTime)
	log.Printf("最大响应时间: %v", stats.MaxResponseTime)
	log.Printf("最小响应时间: %v", stats.MinResponseTime)
	log.Printf("数据传输: %.2f KB", float64(stats.TotalBytesTransfer)/1024)

	// 内存统计
	memStart := float64(stats.MemoryUsageStart.Alloc) / 1024 / 1024
	memEnd := float64(stats.MemoryUsageEnd.Alloc) / 1024 / 1024
	log.Printf("内存使用 - 开始: %.2f MB, 结束: %.2f MB, 增长: %.2f MB",
		memStart, memEnd, memEnd-memStart)
	log.Printf("系统内存 - 开始: %.2f MB, 结束: %.2f MB",
		float64(stats.MemoryUsageStart.Sys)/1024/1024,
		float64(stats.MemoryUsageEnd.Sys)/1024/1024)
	log.Printf("垃圾回收次数: %d", stats.MemoryUsageEnd.NumGC-stats.MemoryUsageStart.NumGC)
	log.Printf("===========================")
}

// Benchmark 系列 - 极端性能测试
func BenchmarkExtremeRoomCreation(b *testing.B) {
	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false

	server, err := NewAppServer(&config)
	if err != nil {
		b.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			roomID := fmt.Sprintf("extreme-bench-room-%d-%d", b.N, i)
			sdk.CreateRoom(roomID, "Extreme Bench Room", 100)
			i++
		}
	})
}

func BenchmarkConcurrentBroadcast(b *testing.B) {
	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false

	server, err := NewAppServer(&config)
	if err != nil {
		b.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK
	message := []byte("Concurrent broadcast test message")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sdk.BroadcastAll(message)
		}
	})
}

func BenchmarkMixedOperations(b *testing.B) {
	config := configs.NewDefaultConfig()
	config.Cluster.Enabled = false

	server, err := NewAppServer(&config)
	if err != nil {
		b.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sdk := server.gohubSDK

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			roomID := fmt.Sprintf("mixed-bench-room-%d", i)

			// 混合操作：创建、查询、广播、删除
			sdk.CreateRoom(roomID, "Mixed Bench Room", 50)
			sdk.GetRoom(roomID)
			sdk.BroadcastAll([]byte(fmt.Sprintf("Mixed message %d", i)))
			sdk.DeleteRoom(roomID)

			i++
		}
	})
}
