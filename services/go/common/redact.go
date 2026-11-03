package common

import (
	"io"
	"log"
	"os"
	"regexp"
)

// Credentials embedded in URLs/DSNs (redis://:pw@h, postgres://u:pw@h,
// amqp://..., key=value DSNs) must never reach the logs.
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

// Every service imports common, so the standard logger is redacted
// process-wide (including log.Fatalf of driver errors that echo a DSN).
func init() { log.SetOutput(RedactingWriter(os.Stderr)) }
