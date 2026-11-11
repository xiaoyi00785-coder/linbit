package common

// redact.go：日志里不能出现 URL/DSN 中的口令。本包被各服务导入，init 会把标准日志包一层。

import (
	"io"
	"log"
	"os"
	"regexp"
)

// URL/DSN 里的口令不能进日志：redis://:pw@h、postgres://u:pw@h、amqp://，以及 key=value 形式的 password。
var (
	reURLCred = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^\s:/@]*):[^\s/]*@`)
	reKVPass  = regexp.MustCompile(`(?i)\b(password|passwd|pwd)=('[^']*'|[^\s&]*)`)
)

// RedactSecrets masks the password part of any URL/DSN found in s.
func RedactSecrets(s string) string {
	s = reURLCred.ReplaceAllString(s, "$1:***@")
	return reKVPass.ReplaceAllString(s, "$1=***")
}

type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) {
	if _, err := r.w.Write([]byte(RedactSecrets(string(p)))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// RedactingWriter wraps w so every write has URL/DSN passwords masked.
func RedactingWriter(w io.Writer) io.Writer { return redactWriter{w} }

// 各服务都会导入 common，所以标准日志在整个进程里打码，包括驱动把 DSN 打进 log.Fatalf 的情况。
func init() { log.SetOutput(RedactingWriter(os.Stderr)) }
