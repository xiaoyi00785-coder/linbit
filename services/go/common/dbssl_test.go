package common

import "testing"

func TestDBSSLProblems(t *testing.T) {
	ok := []string{
		"postgres://u:p@postgres:5432/linbit?sslmode=disable",
		"postgres://u:p@10.0.0.5:5432/linbit?sslmode=disable",
		"postgres://u:p@172.18.0.2/linbit",
		"postgres://u:p@localhost/linbit?sslmode=disable",
		"postgres://u:p@db.example.com/linbit?sslmode=require",
		"postgres://u:p@db.svc.internal/linbit?sslmode=disable",
	}
	bad := []string{
		"postgres://u:p@db.example.com/linbit?sslmode=disable",
		"postgres://u:p@203.0.113.7/linbit",
		"postgres://u:p@8.8.8.8/linbit?sslmode=prefer",
	}
	for _, d := range ok {
		if p := DBSSLProblems(d); len(p) != 0 {
			t.Errorf("%s: unexpected %v", d, p)
		}
	}
	for _, d := range bad {
		if p := DBSSLProblems(d); len(p) == 0 {
			t.Errorf("%s: expected problem", d)
		}
	}
}
