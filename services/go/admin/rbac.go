package main

// Roles (DESIGN §3). An admin may hold several roles.
const (
	roleSuper         = "super_admin"
	roleOperations    = "operations"
	roleKYCReviewer   = "kyc_reviewer"
	roleFinanceFirst  = "finance_first"
	roleFinanceSecond = "finance_second"
	roleRisk          = "risk"
	roleSupport       = "support"
	roleMarketing     = "marketing"
	roleAuditor       = "auditor"
)

// Permission codes (module:action).
const (
	permUserView       = "user:view"
	permUserEdit       = "user:edit"
	permUserPII        = "user:pii"
	permKYCView        = "kyc:view"
	permKYCApprove     = "kyc:approve"
	permWithdrawView   = "withdraw:view"
	permWithdrawFirst  = "withdraw:first"
	permWithdrawSecond = "withdraw:second"
	permLedgerView     = "ledger:view"
	permSymbolView     = "symbol:view"
	permSymbolEdit     = "symbol:edit"
	permFeeView        = "fee:view"
	permFeeEdit        = "fee:edit"
	permFeeApprove     = "fee:approve"
	permRewardView     = "reward:view"
	permRewardEdit     = "reward:edit"
	permRewardApprove  = "reward:approve"
	permCouponApprove  = "coupon:approve"
	permContentEdit    = "content:edit"
	permAuditView      = "audit:view"
	permAdminManage    = "admin:manage"
)

var allPerms = []string{permUserView, permUserEdit, permUserPII, permKYCView, permKYCApprove,
	permWithdrawView, permWithdrawFirst, permWithdrawSecond, permLedgerView, permSymbolView,
	permSymbolEdit, permFeeView, permFeeEdit, permFeeApprove, permRewardView, permRewardEdit,
	permRewardApprove, permCouponApprove, permContentEdit, permAuditView, permAdminManage}

// rolePerms is the RBAC matrix from prototypes/admin/DESIGN.md §3.
var rolePerms = map[string][]string{
	roleSuper:         allPerms,
	roleOperations:    {permUserView, permUserEdit, permKYCView, permSymbolView, permRewardView, permRewardEdit, permContentEdit},
	roleKYCReviewer:   {permUserView, permKYCView, permKYCApprove},
	roleFinanceFirst:  {permUserView, permWithdrawView, permWithdrawFirst, permLedgerView, permCouponApprove, permRewardView},
	roleFinanceSecond: {permUserView, permFeeView, permWithdrawView, permWithdrawSecond, permLedgerView, permFeeApprove, permCouponApprove, permRewardView, permRewardApprove},
	roleRisk:          {permUserView, permUserEdit, permKYCView, permWithdrawView, permLedgerView},
	roleSupport:       {permUserView, permKYCView, permLedgerView},
	roleMarketing:     {permRewardView, permRewardEdit, permContentEdit},
	roleAuditor:       {permUserView, permKYCView, permWithdrawView, permLedgerView, permSymbolView, permFeeView, permRewardView, permAuditView},
}

func validRole(r string) bool { _, ok := rolePerms[r]; return ok }

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func can(roles []string, perm string) bool {
	for _, r := range roles {
		for _, p := range rolePerms[r] {
			if p == perm {
				return true
			}
		}
	}
	return false
}
