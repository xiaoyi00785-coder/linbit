package main

import "testing"

func TestColdSweepPermLockedToSuper(t *testing.T) {
	if !can([]string{roleSuper}, permWalletColdSweep) {
		t.Fatal("super_admin must have wallet:cold_sweep")
	}
	if !lockedPerms[permWalletColdSweep] {
		t.Fatal("wallet:cold_sweep must be in lockedPerms")
	}
	for role, perms := range rolePerms {
		if role == roleSuper {
			continue
		}
		if can([]string{role}, permWalletColdSweep) {
			t.Fatalf("role %s must not have wallet:cold_sweep", role)
		}
		for _, p := range perms {
			if p == permWalletColdSweep {
				t.Fatalf("role %s matrix includes locked cold_sweep", role)
			}
		}
	}
}
