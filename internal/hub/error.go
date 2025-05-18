package hub

import (
	"encoding/json"
	"fmt"
)

// 错误码定义
const (
	// 系统相关错误码 (1000-1999)
	ErrCodeUnknown          = 1000 // 未知错误
	ErrCodeInvalidFormat    = 1001 // 无效的消息格式
	ErrCodeInvalidMessageID = 1002 // 无效的消息ID
	ErrCodeServerError      = 1003 // 服务器内部错误
	ErrCodeTimeout          = 1004 // 操作超时
	ErrCodeRateLimited      = 1005 // 请求频率限制

	// 认证相关错误码 (2000-2999)
	ErrCodeUnauthorized = 2000 // 未授权
	ErrCodeForbidden    = 2001 // 禁止访问
	ErrCodeTokenExpired = 2002 // 令牌过期
	ErrCodeInvalidToken = 2003 // 无效的令牌

	// 房间相关错误码 (3000-3999)
	ErrCodeRoomNotFound      = 3000 // 房间不存在
	ErrCodeRoomAlreadyExists = 3001 // 房间已存在
	ErrCodeRoomFull          = 3002 // 房间已满
	ErrCodeNotInRoom         = 3003 // 不在房间中
	ErrCodeAlreadyInRoom     = 3004 // 已在房间中

	// 客户端相关错误码 (4000-4999)
	ErrCodeClientNotFound  = 4000 // 客户端不存在
	ErrCodeClientOffline   = 4001 // 客户端离线
	ErrCodeMessageTooLarge = 4002 // 消息过大
)

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	Code      int    `json:"code"`              // 错误码
	Message   string `json:"message"`           // 错误信息
	RequestID int    `json:"request_id"`        // 请求ID，用于关联请求和响应
	Details   any    `json:"details,omitempty"` // 详细错误信息（可选）
}

// NewError 创建新的错误响应
func NewError(code int, message string, requestID int, details any) *ErrorResponse {
	return &ErrorResponse{
		Code:      code,
		Message:   message,
		RequestID: requestID,
		Details:   details,
	}
}

// Error 实现error接口
func (e *ErrorResponse) Error() string {
	if e.Details != nil {
		return fmt.Sprintf("error code: %d, message: %s, details: %v", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("error code: %d, message: %s", e.Code, e.Message)
}

// ToJSON 将错误响应转换为JSON
func (e *ErrorResponse) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// SendError 向客户端发送错误响应
func SendError(client *Client, err *ErrorResponse) error {
	// 创建错误消息
	errMsg, err2 := NewMessage(0, "error", err)
	if err2 != nil {
		return err2
	}

	// 序列化消息
	data, err2 := errMsg.Encode()
	if err2 != nil {
		return err2
	}

	// 发送到客户端
	return client.Send(Frame{
		MsgType: 1, // 文本消息
		Data:    data,
	})
}

// 映射内部错误到标准错误码
func MapErrorToCode(err error) int {
	switch err {
	case ErrClientNotFound:
		return ErrCodeClientNotFound
	case ErrRoomNotFound:
		return ErrCodeRoomNotFound
	case ErrRoomAlreadyExists:
		return ErrCodeRoomAlreadyExists
	case ErrRoomIsFull:
		return ErrCodeRoomFull
	case ErrClientNotInRoom:
		return ErrCodeNotInRoom
	case ErrClientAlreadyInRoom:
		return ErrCodeAlreadyInRoom
	default:
		return ErrCodeUnknown
	}
}
