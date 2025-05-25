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
	var err error

	// 初始尝试
	err = operation()
	if err == nil {
		slog.Info("operation successful on initial attempt", "operation", operationName)
		return nil
	}
	slog.Warn("initial attempt failed, will retry", "operation", operationName, "error", err, "max_retries", maxRetries)

	if maxRetries <= 0 { // 不配置重试
		slog.Error("operation failed on initial attempt and no retries configured", "operation", operationName, "error", err)
		return fmt.Errorf("%s failed on initial attempt (no retries): %w", operationName, err)
	}

	currentBackoff := initialBackoff
	for i := 0; i < maxRetries; i++ {
		retryNum := i + 1
		slog.Info("waiting for backoff before retrying operation",
			"operation", operationName,
			"retry_attempt", retryNum,
			"total_retries_allowed", maxRetries,
			"backoff_duration_ms", currentBackoff.Milliseconds())

		select {
		case <-ctx.Done():
			slog.Warn("context cancelled during backoff before retry", "operation", operationName, "retry_attempt", retryNum, "error", ctx.Err())
			return ctx.Err()
		case <-time.After(currentBackoff):
		}

		slog.Info("retrying operation", "operation", operationName, "retry_attempt", retryNum)
		err = operation()
		if err == nil {
			slog.Info("operation successful after retry", "operation", operationName, "retry_attempt", retryNum)
			return nil // 成功
		}

		slog.Warn("retry attempt failed",
			"operation", operationName,
			"error", err,
			"retry_attempt", retryNum,
			"total_retries_allowed", maxRetries)

		// 如果不是最后一次重试，则增加下一次重试的退避时间
		if i < maxRetries-1 {
			currentBackoff = time.Duration(float64(currentBackoff) * 1.5)
			if currentBackoff > maxBackoff {
				currentBackoff = maxBackoff
			}
		}
	}

	finalErr := fmt.Errorf("%s failed after initial attempt and %d retries: %w", operationName, maxRetries, err)
	slog.Error("operation failed after all retries",
		"operation", operationName,
		"error_from_last_attempt", err,
		"total_retries_executed", maxRetries,
		"final_error_message", finalErr.Error())
	return finalErr
}
