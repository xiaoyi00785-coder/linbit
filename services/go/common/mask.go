package common

// mask.go：邮箱和手机号的平台统一打码，规则和网页端一致。

import "strings"

// MaskEmail 是全平台邮箱打码，和网页 apps/web/lib/account.ts 的 maskEmail 一致：
// 本地部分保留 min(3, max(1, len(local)-1)) 个字符，然后 "***@domain"。
//
//	alice@x.io -> ali***@x.io，ab@x.io -> a***@x.io，a@x.io -> a***@x.io
//
// 没有本地部分的值原样返回。
func MaskEmail(e string) string {
	at := strings.IndexByte(e, '@')
	if at <= 0 {
		return e
	}
	name := []rune(e[:at])
	keep := len(name) - 1
	if keep > 3 {
		keep = 3
	}
	if keep < 1 {
		keep = 1
	}
	return string(name[:keep]) + "***" + e[at:]
}

// MaskPhone 是全平台手机打码，和网页一致：
// 超过 7 位：前 3 + "****" + 后 4；5–7 位：前 2 + "***" + 后 2；更短："****"。
func MaskPhone(p string) string {
	n := len(p)
	switch {
	case n > 7:
		return p[:3] + "****" + p[n-4:]
	case n > 4:
		return p[:2] + "***" + p[n-2:]
	default:
		return "****"
	}
}
