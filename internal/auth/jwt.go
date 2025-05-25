package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
	UserID      string       `json:"user_id"`
	Username    string       `json:"username"`
	Permissions []Permission `json:"permissions"`
	jwt.RegisteredClaims
}

type JWTService struct {
	secretKey []byte
	issuer    string
}

func NewJWTService(secretKey, issuer string) *JWTService {
	return &JWTService{
		secretKey: []byte(secretKey),
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
	tokenString, err := token.SignedString(s.secretKey)
	if err != nil {
		slog.ErrorContext(ctx, "无法签名JWT令牌", "error", err, "userID", userID)
		return "", fmt.Errorf("无法签名令牌: %w", err)
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
		// 验证签名算法是否为预期的 HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期的签名算法: %v", token.Header["alg"])
		}
		return s.secretKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenMalformed) {
			slog.WarnContext(ctx, "JWT令牌格式错误", "error", err)
			return nil, ErrInvalidToken
		} else if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			slog.WarnContext(ctx, "JWT令牌签名无效", "error", err)
			return nil, ErrInvalidToken
		} else if errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenNotValidYet) {
			slog.InfoContext(ctx, "JWT令牌已过期或尚未生效", "error", err)
			return nil, ErrTokenExpired
		} else {
			slog.ErrorContext(ctx, "JWT令牌解析/验证失败", "error", err)
			return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err) // 包装原始错误
		}
	}

	if !token.Valid {
		slog.WarnContext(ctx, "JWT令牌验证未通过，尽管解析时未报告致命错误")
		return nil, ErrInvalidToken
	}

	// 获取JWT声明
	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		slog.ErrorContext(ctx, "无法将token.Claims断言为*JWTClaims")
		return nil, ErrInvalidToken
	}

	// （可选）验证 Issuer 是否匹配
	if claims.Issuer != s.issuer {
		slog.WarnContext(ctx, "JWT令牌签发者不匹配", "expected_issuer", s.issuer, "actual_issuer", claims.Issuer)
		return nil, ErrInvalidToken
	}

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
	return HasPermission(claims, permission)
}

var _ Authenticator = (*JWTService)(nil)
var _ Authorizer = (*JWTService)(nil)
