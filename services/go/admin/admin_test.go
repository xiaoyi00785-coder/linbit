package main

import "testing"

func TestRBACMatrix(t *testing.T) {
	cases := []struct {
		roles []string
		perm  string
		want  bool
	}{
		{[]string{roleSuper}, permAdminManage, true},
		{[]string{roleMarketing}, permUserView, false},
		{[]string{roleMarketing}, permRewardApprove, false},
		{[]string{roleFinanceFirst}, permWithdrawSecond, false},
		{[]string{roleFinanceSecond}, permWithdrawFirst, false},
		{[]string{roleFinanceSecond}, permFeeApprove, true},
		{[]string{roleFinanceFirst}, permFeeApprove, false},
		{[]string{roleKYCReviewer}, permUserPII, false},
		{[]string{roleAuditor}, permAuditView, true},
		{[]string{roleAuditor}, permUserEdit, false},
		{[]string{roleKYCReviewer, roleMarketing}, permRewardEdit, true},
		{nil, permUserView, false},
		{[]string{"nope"}, permUserView, false},
	}
	for _, c := range cases {
		if got := can(c.roles, c.perm); got != c.want {
			t.Errorf("can(%v,%s)=%v want %v", c.roles, c.perm, got, c.want)
		}
	}
	if validRole("god") || !validRole(roleSuper) {
		t.Fatal("validRole")
	}
	// Only super_admin may edit fees; nobody else holds admin:manage.
	for r, ps := range rolePerms {
		if r == roleSuper {
			continue
		}
		for _, p := range ps {
			if p == permFeeEdit || p == permAdminManage || p == permUserPII {
				t.Errorf("role %s unexpectedly holds %s", r, p)
			}
		}
	}
}

func TestMasking(t *testing.T) {
	for in, want := range map[string]string{
		"alice@example.com": "ali***@example.com",
		"ab@x.io":           "a***@x.io",
		"bad":               "bad",
	} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q)=%q want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"13812345678": "138****5678",
		"12345":       "12***45",
		"123":         "****",
	} {
		if got := maskPhone(in); got != want {
			t.Errorf("maskPhone(%q)=%q want %q", in, got, want)
		}
	}
	m := map[string]interface{}{"email": "alice@example.com", "phone": "13812345678"}
	maskUser(m)
	if m["email"] != "ali***@example.com" || m["phone"] != "138****5678" {
		t.Errorf("maskUser: %v", m)
	}
}

func TestTextArray(t *testing.T) {
	var a textArray
	for in, want := range map[string]int{`{}`: 0, `{a,b}`: 2, `{"x y",z}`: 2} {
		if err := a.Scan(in); err != nil || len(a) != want {
			t.Errorf("Scan(%q)=%v,%v", in, a, err)
		}
	}
	if err := a.Scan([]byte(`{super_admin,finance_second}`)); err != nil || a[1] != "finance_second" {
		t.Errorf("Scan bytes: %v %v", a, err)
	}
	v, err := textArray{"a", "b"}.Value()
	if err != nil || v == nil {
		t.Fatalf("Value: %v %v", v, err)
	}
	if err := a.Scan(v); err != nil || len(a) != 2 || a[0] != "a" {
		t.Errorf("roundtrip: %v %v", a, err)
	}
}
