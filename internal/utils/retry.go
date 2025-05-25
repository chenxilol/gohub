package utils

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetryWithBackoff 尝试执行 'operation'。如果失败，它会以指数退避的方式重试 'maxRetries' 次。
// operationName 用于日志记录。
// initialBackoff 是初始的退避时间。
// maxBackoff 是最大的退避时间。
// operation 是要执行的函数，如果成功则返回 nil，否则返回错误。
func RetryWithBackoff(ctx context.Context, operationName string, maxRetries int, initialBackoff time.Duration, maxBackoff time.Duration, operation func() error) error {
	// 初始尝试
	if err := operation(); err == nil {
		slog.Debug("operation successful on initial attempt", "operation", operationName)
		return nil
	} else if maxRetries <= 0 {
		slog.Error("operation failed with no retries configured", "operation", operationName, "error", err)
		return fmt.Errorf("%s failed (no retries): %w", operationName, err)
	} else {
		slog.Debug("initial attempt failed, will retry", "operation", operationName, "error", err)
	}

	currentBackoff := initialBackoff
	for i := 0; i < maxRetries; i++ {
		retryNum := i + 1
		slog.Debug("waiting before retry", "operation", operationName, "attempt", retryNum, "backoff_ms", currentBackoff.Milliseconds())

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(currentBackoff):
		}

		if err := operation(); err == nil {
			slog.Debug("operation successful after retry", "operation", operationName, "attempt", retryNum)
			return nil
		} else {
			slog.Debug("retry attempt failed", "operation", operationName, "error", err, "attempt", retryNum)
		}

		// 计算下一次重试的退避时间
		if i < maxRetries-1 {
			currentBackoff = time.Duration(float64(currentBackoff) * 1.5)
			if currentBackoff > maxBackoff {
				currentBackoff = maxBackoff
			}
		}
	}

	finalErr := fmt.Errorf("%s failed after %d retries", operationName, maxRetries)
	slog.Error("operation failed after all retries", "operation", operationName, "retries", maxRetries)
	return finalErr
}
