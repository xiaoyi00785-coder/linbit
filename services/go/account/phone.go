package main

import (
	"regexp"
	"strings"
)

// e164Re matches an E.164 phone number: a leading '+', a non-zero leading digit,
// then up to 14 more digits (15 digits total maximum).
var e164Re = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// dialCodeRe matches a bare country dial code (1–3 digits, no '+').
var dialCodeRe = regexp.MustCompile(`^[1-9]\d{0,2}$`)

// normalizePhone composes and validates an E.164 phone number.
//
// Callers may pass the full number already in E.164 ("+8613800138000"), in which
// case countryCode is optional. Otherwise a bare national number plus a dial
// countryCode ("86" + "13800138000") is composed into E.164. It returns the
// normalized "+<digits>" string and the dial code, or ok=false if invalid.
func normalizePhone(phone, countryCode string) (normalized, dial string, ok bool) {
	phone = strings.TrimSpace(phone)
	countryCode = strings.TrimSpace(strings.TrimPrefix(countryCode, "+"))

	if strings.HasPrefix(phone, "+") {
		if !e164Re.MatchString(phone) {
			return "", "", false
		}
		return phone, countryCode, true
	}
	// Bare national number: a dial code is required to build E.164.
	if !dialCodeRe.MatchString(countryCode) {
		return "", "", false
	}
	// Drop spaces, dashes and a possible leading trunk '0' from the national part.
	national := strings.NewReplacer(" ", "", "-", "").Replace(phone)
	if national == "" || !isAllDigits(national) {
		return "", "", false
	}
	candidate := "+" + countryCode + national
	if !e164Re.MatchString(candidate) {
		return "", "", false
	}
	return candidate, countryCode, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validEmail is a light sanity check (presence of a single '@' with non-empty
// local and domain parts); the real gate is the verification code.
func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.IndexByte(email, '@')
	if at <= 0 || at != strings.LastIndexByte(email, '@') {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}
