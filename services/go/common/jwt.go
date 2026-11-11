package common

// jwt.go：HS256 access token。Subject 是 uid；子账户带 acct=sub 和 parent。

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken 表示 access token 校验失败。签名算法不对、过期、typ 不是 access 都归这里。
var ErrInvalidToken = errors.New("invalid token")

// AccessClaims 是 access token 里的自定义声明。Subject 是对外的用户 uid。
type AccessClaims struct {
	Email string `json:"email"`
	Typ   string `json:"typ"`
	// Acct 为 "sub" 时这是子账户会话，Parent 是父账户 uid。普通账户这两项为空。
	// 网关用它们拦住子账户访问仅父账户可用的接口，以及充值、提现。
	Acct   string `json:"acct,omitempty"`
	Parent string `json:"parent,omitempty"`
	// IatMs 是签发时间的 unix 毫秒，用来和该用户的登出截止时间比较（见 revoke.go）。
	// 没有这个字段的旧令牌回退到秒级 iat。
	IatMs int64 `json:"iatms,omitempty"`
	jwt.RegisteredClaims
}

// SignAccessToken 签发 HS256 access token。typ 固定为 access，iss 为 linbit。
func SignAccessToken(secret, uid, email string, ttl time.Duration, now time.Time) (string, error) {
	return SignAccessTokenFor(secret, uid, email, "", ttl, now)
}

// SignAccessTokenFor 签发 access token。parentUID 非空时记成子账户会话（acct="sub"）。
func SignAccessTokenFor(secret, uid, email, parentUID string, ttl time.Duration, now time.Time) (string, error) {
	acct := ""
	if parentUID != "" {
		acct = "sub"
	}
	claims := AccessClaims{
		Email:  email,
		Typ:    "access",
		Acct:   acct,
		Parent: parentUID,
		IatMs:  now.UnixMilli(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uid,
			Issuer:    "linbit",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// ParseAccessToken 校验 HS256 access token。算法不是 HMAC，或 typ 不是 access，返回 ErrInvalidToken。
func ParseAccessToken(secret, token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Typ != "access" {
		return nil, fmt.Errorf("%w: wrong token type", ErrInvalidToken)
	}
	return claims, nil
}
