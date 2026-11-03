package main

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// textArray maps a Postgres TEXT[] of simple identifiers (role names) through
// database/sql. Elements must not contain commas, quotes, braces or spaces —
// roles are validated against the RBAC matrix before they are stored.
type textArray []string

func (a textArray) Value() (driver.Value, error) {
	return "{" + strings.Join(a, ",") + "}", nil
}

func (a *textArray) Scan(src interface{}) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case nil:
		*a = nil
		return nil
	default:
		return fmt.Errorf("textArray: unsupported %T", src)
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.Trim(p, `"`); p != "" {
			out = append(out, p)
		}
	}
	*a = out
	return nil
}
