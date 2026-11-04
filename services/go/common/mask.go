package common

import "strings"

// MaskEmail is the platform-wide email mask, identical to the web client's
// maskEmail (apps/web/lib/account.ts): keep min(3, max(1, len(local)-1))
// leading characters of the local part, then "***@domain".
//
//	alice@x.io -> ali***@x.io, ab@x.io -> a***@x.io, a@x.io -> a***@x.io
//
// A value without a local part is returned unchanged.
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

// MaskPhone is the platform-wide phone mask, matching the web client:
// >7 digits: first 3 + "****" + last 4; 5–7: first 2 + "***" + last 2;
// shorter: "****".
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
