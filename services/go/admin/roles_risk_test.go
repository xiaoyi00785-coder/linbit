package main

import "testing"

func TestFinalPermsKeepsLockedDefaults(t *testing.T) {
	// finance_second has withdraw:second by default; dropping it must not stick, adding fund perms to support must not stick.
	got := finalPerms(roleFinanceSecond, []string{permDashView})
	want := map[string]bool{permDashView: true, permFundAdjust: true, permFundAdjustApprove: true, permWithdrawSecond: true, permFeeApprove: true, permCouponApprove: true, permRewardApprove: true, permConfigApprove: true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected %s in %v", p, got)
		}
	}
	for _, p := range finalPerms(roleSupport, []string{permSupportView, permWithdrawSecond, permFundAdjust, permAdminManage, permUserEdit}) {
		if lockedPerms[p] {
			t.Fatalf("locked perm %s leaked into support", p)
		}
	}
	for _, p := range finalPerms("custom_x", []string{permAuditView, permFundAdjustApprove}) {
		if lockedPerms[p] {
			t.Fatalf("locked perm %s leaked into custom role", p)
		}
	}
}

func TestNormParams(t *testing.T) {
	if _, _, _, err := normParams("large_withdrawal", map[string]interface{}{"assets": "USDT", "minAmount": 100.0, "lookbackHours": 24.0, "cooldownHours": 24.0, "excludeUids": ""}); err != nil {
		t.Fatal(err)
	}
	bad := []map[string]interface{}{
		{"assets": "USDT", "minAmount": -1.0, "lookbackHours": 24.0, "cooldownHours": 1.0},
		{"assets": "USDT'; drop", "minAmount": 1.0, "lookbackHours": 24.0, "cooldownHours": 1.0},
		{"assets": "USDT", "minAmount": 1.0, "lookbackHours": 24.0, "cooldownHours": 1.0, "evil": 1.0},
		{"assets": "", "minAmount": 1.0, "lookbackHours": 24.0, "cooldownHours": 1.0},
	}
	for i, b := range bad {
		if _, _, _, err := normParams("large_withdrawal", b); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	for k := range riskKinds {
		if riskKinds[k].Detect == nil {
			t.Fatalf("%s has no detector", k)
		}
	}
}
