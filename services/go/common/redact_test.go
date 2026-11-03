package common

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := map[string]string{
		"redis: connected to redis://:s3cr3t@redis:6379/0":            "redis: connected to redis://:***@redis:6379/0",
		"redis://default:p@ss@redis:6379":                             "redis://default:***@redis:6379",
		"open db: postgres://linbit:hunter2@db:5432/linbit?sslmode=x": "open db: postgres://linbit:***@db:5432/linbit?sslmode=x",
		"dsn host=db user=u password=hunter2 dbname=x":                "dsn host=db user=u password=*** dbname=x",
		"engine=http://matching-engine:8080 user@example.com":         "engine=http://matching-engine:8080 user@example.com",
	}
	for in, want := range cases {
		if got := RedactSecrets(in); got != want {
			t.Errorf("RedactSecrets(%q) = %q, want %q", in, got, want)
		}
	}
	var buf bytes.Buffer
	l := log.New(RedactingWriter(&buf), "", 0)
	l.Printf("redis: connected to %s", "rediss://:topsecret@r:6380")
	if strings.Contains(buf.String(), "topsecret") {
		t.Fatalf("leaked: %s", buf.String())
	}
}
