package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT令牌的声明
type JWTClaims struct {
	UserID      string       `json:"user_id"`
	Username    string       `json:"username"`
	Permissions []Permission `json:"permissions"`
	jwt.RegisteredClaims
}

// JWTService JWT认证与授权服务
type JWTService struct {
	secretKey string
	issuer    string
}

// NewJWTService 创建一个新的JWT服务
func NewJWTService(secretKey, issuer string) *JWTService {
	return &JWTService{
		secretKey: secretKey,
		issuer:    issuer,
	}
}

// GenerateToken 实现Authenticator接口，生成JWT令牌
func (s *JWTService) GenerateToken(ctx context.Context, userID, username string, permissions []Permission, expiration time.Duration) (string, error) {
	now := time.Now()
	expirationTime := now.Add(expiration)

	// 创建JWT声明
	claims := JWTClaims{
		UserID:      userID,
		Username:    username,
		Permissions: permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    s.issuer,
			Subject:   userID,
		},
	}

	// 创建令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 使用密钥签名令牌
	tokenString, err := token.SignedString([]byte(s.secretKey))
	if err != nil {
		return "", fmt.Errorf("could not sign the token: %w", err)
	}

	return tokenString, nil
}

// Authenticate 实现Authenticator接口，验证JWT令牌
func (s *JWTService) Authenticate(ctx context.Context, tokenString string) (*TokenClaims, error) {
	// 检查令牌是否为空
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// 解析令牌
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.secretKey), nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not parse token: %w", err)
	}

	// 检查令牌是否有效
	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// 获取JWT声明
	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	// 转换为通用TokenClaims
	return &TokenClaims{
		UserID:      claims.UserID,
		Username:    claims.Username,
		Permissions: claims.Permissions,
		ExpiresAt:   claims.ExpiresAt.Unix(),
		IssuedAt:    claims.IssuedAt.Unix(),
		Issuer:      claims.Issuer,
	}, nil
}

// CheckPermission 实现Authorizer接口，检查权限
func (s *JWTService) CheckPermission(claims *TokenClaims, permission Permission, resource string) bool {
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

// 确保JWTService实现了Authenticator和Authorizer接口
var _ Authenticator = (*JWTService)(nil)
var _ Authorizer = (*JWTService)(nil)
