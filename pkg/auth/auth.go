package auth

import (
	"context"
	"errors"
	"slices"
	"time"
)

var (
	ErrInvalidToken       = errors.New("无效的令牌")
	ErrTokenExpired       = errors.New("令牌已过期")
	ErrPermissionDenied   = errors.New("权限不足")
	ErrInvalidCredentials = errors.New("无效的凭证")
)

type Permission string

const (
	PermReadMessage Permission = "read:message" // 读取消息权限
	PermSendMessage Permission = "send:message" // 发送消息权限
	PermCreateRoom  Permission = "create:room"  // 创建房间权限
	PermDeleteRoom  Permission = "delete:room"  // 删除房间权限
	PermJoinRoom    Permission = "join:room"    // 加入房间权限
	PermLeaveRoom   Permission = "leave:room"   // 离开房间权限
	PermAdminRoom   Permission = "admin:room"   // 房间管理权限
	PermAdminSystem Permission = "admin:system" // 系统管理权限
)

// TokenClaims 定义令牌的声明内容 (通用结构，由具体的认证实现填充)
type TokenClaims struct {
	UserID      string       `json:"user_id"`     // 用户ID
	Username    string       `json:"username"`    // 用户名
	Permissions []Permission `json:"permissions"` // 权限列表
	ExpiresAt   int64        `json:"exp"`         // 过期时间 (Unix timestamp)
	IssuedAt    int64        `json:"iat"`         // 签发时间 (Unix timestamp)
	Issuer      string       `json:"iss"`         // 签发者
}

// Authenticator 认证接口，用于解析和验证令牌
type Authenticator interface {
	// Authenticate 验证令牌并返回令牌声明
	Authenticate(ctx context.Context, token string) (*TokenClaims, error)

	// GenerateToken 生成新的令牌
	GenerateToken(ctx context.Context, userID, username string, permissions []Permission, expiration time.Duration) (string, error)
}

// Authorizer 授权接口，用于检查权限
type Authorizer interface {
	// CheckPermission 检查令牌是否有特定权限
	CheckPermission(claims *TokenClaims, permission Permission, resource string) bool
}

// HasPermission 是一个辅助函数，用于检查给定的 TokenClaims 是否包含指定的任一权限。
// 如果用户拥有 PermAdminSystem 权限，则视为拥有所有权限。
func HasPermission(claims *TokenClaims, permissions ...Permission) bool {
	if claims == nil {
		return false
	}

	// 系统管理员拥有所有权限
	if slices.Contains(claims.Permissions, PermAdminSystem) {
		return true
	}

	// 检查特定权限
	permissionMap := make(map[Permission]bool)
	for _, p := range claims.Permissions {
		permissionMap[p] = true
	}

	for _, requestedPermission := range permissions {
		if permissionMap[requestedPermission] {
			return true
		}
	}

	return false
}
