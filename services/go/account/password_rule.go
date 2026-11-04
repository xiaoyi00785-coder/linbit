package main

import "unicode"

// passwordRuleMsg is the user-facing description of the login password rule
// (8–64 characters, at least one letter and one digit). Kept in sync with the
// web register / reset / change-password forms.
const passwordRuleMsg = "密码需为 8-64 位，且同时包含字母和数字"

// validLoginPassword reports whether p satisfies the login password rule.
// Length is counted in characters (runes), not bytes.
func validLoginPassword(p string) bool {
	n := 0
	var letter, digit bool
	for _, r := range p {
		n++
		switch {
		case r < 0x80 && unicode.IsLetter(r):
			letter = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	return n >= 8 && n <= 64 && letter && digit
}
