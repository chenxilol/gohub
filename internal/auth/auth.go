// Package auth 提供认证和授权功能
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// 定义错误
	ErrInvalidToken       = errors.New("无效的令牌")
	ErrTokenExpired       = errors.New("令牌已过期")
	ErrPermissionDenied   = errors.New("权限不足")
	ErrInvalidCredentials = errors.New("无效的凭证")
)

// 定义权限类型
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

// TokenClaims 定义令牌的声明内容
type TokenClaims struct {
	UserID      string       `json:"user_id"`     // 用户ID
	Username    string       `json:"username"`    // 用户名
	Permissions []Permission `json:"permissions"` // 权限列表
	ExpiresAt   int64        `json:"exp"`         // 过期时间
	IssuedAt    int64        `json:"iat"`         // 签发时间
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

// AuthService 认证与授权服务，实现Authenticator和Authorizer接口
type AuthService struct {
	secretKey string
	issuer    string
}

// NewAuthService 创建一个新的认证与授权服务
func NewAuthService(secretKey, issuer string) *AuthService {
	return &AuthService{
		secretKey: secretKey,
		issuer:    issuer,
	}
}

// 实现Authenticator接口的Authenticate方法
func (s *AuthService) Authenticate(ctx context.Context, token string) (*TokenClaims, error) {
	// 检查令牌是否为空
	if token == "" {
		return nil, ErrInvalidToken
	}

	// 在实际实现中，这里应该解析JWT令牌
	// 但为了简单起见，我们这里先实现一个基础版本
	// TODO: 添加完整的JWT解析实现

	// 模拟令牌验证
	claims := &TokenClaims{
		UserID:      "user123",
		Username:    "testuser",
		Permissions: []Permission{PermReadMessage, PermSendMessage, PermJoinRoom, PermLeaveRoom},
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
		IssuedAt:    time.Now().Unix(),
		Issuer:      s.issuer,
	}

	// 检查令牌是否过期
	if claims.ExpiresAt < time.Now().Unix() {
		return nil, ErrTokenExpired
	}

	return claims, nil
}

// 实现Authenticator接口的GenerateToken方法
func (s *AuthService) GenerateToken(ctx context.Context, userID, username string, permissions []Permission, expiration time.Duration) (string, error) {
	// 创建令牌声明
	claims := TokenClaims{
		UserID:      userID,
		Username:    username,
		Permissions: permissions,
		ExpiresAt:   time.Now().Add(expiration).Unix(),
		IssuedAt:    time.Now().Unix(),
		Issuer:      s.issuer,
	}
	fmt.Println(claims)

	// 在实际实现中，这里应该生成JWT令牌
	// 但为了简单起见，我们这里返回一个模拟的令牌字符串
	// TODO: 添加完整的JWT生成实现

	// 模拟令牌生成
	return "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.mockToken", nil
}

// 实现Authorizer接口的CheckPermission方法
func (s *AuthService) CheckPermission(claims *TokenClaims, permission Permission, resource string) bool {
	if claims == nil {
		return false
	}

	// 检查令牌是否包含所需权限
	for _, p := range claims.Permissions {
		if p == permission || p == PermAdminSystem {
			return true
		}
	}

	return false
}

// HasPermission 检查claims是否具有任一权限
func HasPermission(claims *TokenClaims, permissions ...Permission) bool {
	if claims == nil {
		return false
	}

	// 系统管理员拥有所有权限
	for _, p := range claims.Permissions {
		if p == PermAdminSystem {
			return true
		}
	}

	// 检查特定权限
	permissionMap := make(map[Permission]bool)
	for _, p := range claims.Permissions {
		permissionMap[p] = true
	}

	for _, permission := range permissions {
		if permissionMap[permission] {
			return true
		}
	}

	return false
}
