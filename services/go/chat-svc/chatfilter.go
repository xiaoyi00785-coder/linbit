package main

import (
	"regexp"
	"strings"
	"unicode"
)

// chatfilter.go: server-side content checks for chat messages. Text is
// normalised first (full-width -> ASCII, common dot/at look-alikes, zero-width
// characters dropped) so 「ｗｗｗ．ｘｘ．ｃｏｍ」 or "foo(at)bar.com" are caught too.

var (
	reURL   = regexp.MustCompile(`(?i)(https?://|ftp://|www\.|t\.me/|wa\.me/|[a-z0-9][a-z0-9-]*\.(com|net|org|io|cc|me|xyz|top|app|co|cn|info|link|vip|club|site|online|ly|gg|tk|ru|biz|pro|live|shop|fun|icu|ink|ltd|tv|to|im|so|ai)\b)`)
	reEmail = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9-]+(\.[a-z0-9-]+)+`)
	// 9+ digits (dash-separated allowed)
	// (spaces only count after a leading "+", so "65000 70000" price talk passes).
	rePhone = regexp.MustCompile(`\+?\d(?:-?\d){8,}|\+\d[\d\s-]{7,}\d`)
	// Messaging handles: keyword followed by an id-looking token.
	reHandle = regexp.MustCompile(`(?i)(telegram|tg|wechat|weixin|vx|wx|v信|qq|whatsapp|line|discord|skype|微信|威信|薇信|徽信|扣扣|企鹅|电报|飞机|加我|联系我)\s*(号|id)?\s*[:：=]?\s*[@a-z0-9_\-]{4,}`)
	reAt     = regexp.MustCompile(`(?i)(^|\s)@[a-z][a-z0-9_]{4,}`)
)

// normalizeChat maps full-width ASCII to half-width and common obfuscations.
func normalizeChat(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		case r == 0x3000:
			r = ' '
		case r == '。' || r == '．' || r == '点' || r == '點':
			r = '.'
		case r == '＠':
			r = '@'
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF:
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	rep := strings.NewReplacer("(dot)", ".", "[dot]", ".", "(at)", "@", "[at]", "@")
	return rep.Replace(strings.ToLower(out))
}

// chatViolation returns "" when text is acceptable, else an error code.
func chatViolation(text string, cfg ChatConfig) (code, msg string) {
	n := normalizeChat(text)
	if cfg.BlockLinks && reURL.MatchString(n) {
		return "CHAT_LINK_BLOCKED", "聊天室禁止发送链接"
	}
	if cfg.BlockContacts && (reEmail.MatchString(n) || rePhone.MatchString(n) || reHandle.MatchString(n) || reAt.MatchString(n)) {
		return "CHAT_CONTACT_BLOCKED", "聊天室禁止发送联系方式"
	}
	for _, w := range cfg.BannedWords {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(n, w) {
			return "CHAT_WORD_BLOCKED", "消息包含违禁词"
		}
	}
	return "", ""
}

// cleanChatText trims and drops control characters (keeps newlines).
func cleanChatText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
