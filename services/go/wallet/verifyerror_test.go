package main

import "testing"

func TestVerifyErrorSplit(t *testing.T) {
	ts := "2026-11-04T00:00:00Z"
	cases := map[string]struct {
		status int
		code   string
	}{
		"":                       {403, "TWOFA_REQUIRED"},
		"totp_required":          {403, "TWOFA_REQUIRED"},
		"code_required":          {403, "TWOFA_REQUIRED"},
		"no_contact":             {403, "TWOFA_REQUIRED"},
		"totp_invalid":           {401, "INVALID_TOTP"},
		"code_invalid":           {401, "INVALID_TOTP"},
		"withdraw_locked":        {403, "WITHDRAW_LOCKED"},
		"fund_password_required": {400, "FUND_PASSWORD_REQUIRED"},
		"fund_password_invalid":  {400, "FUND_PASSWORD_INVALID"},
		"fund_password_locked":   {423, "FUND_PASSWORD_LOCKED"},
		"fund_password_not_set":  {403, "FUND_PASSWORD_NOT_SET"},
	}
	for reason, want := range cases {
		e := verifyError(VerifyResult{Reason: reason, LockedUntil: &ts})
		if e.status != want.status || e.code != want.code {
			t.Errorf("%q: got %d %s want %d %s", reason, e.status, e.code, want.status, want.code)
		}
	}
}
