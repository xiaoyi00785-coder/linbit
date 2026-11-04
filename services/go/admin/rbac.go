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
	permCopyView       = "copy:view"
	permCopyReview     = "copy:review"
	permPartnerView    = "partner:view"
	permPartnerReview  = "partner:review"
	permAdminManage    = "admin:manage"
	permEarnView       = "earn:view"
	permEarnEdit       = "earn:edit"
	permVIPView        = "vip:view"
	permVIPReview      = "vip:review"
	permMMView         = "mm:view"
	permMMEdit         = "mm:edit"
	permDashView       = "dash:view"
	// 人工调账: create/list, and the second-person approval (maker-checker).
	permFundAdjust        = "fund:adjust"
	permFundAdjustApprove = "fund:adjust:approve"
)

var allPerms = []string{permUserView, permUserEdit, permUserPII, permKYCView, permKYCApprove,
	permWithdrawView, permWithdrawFirst, permWithdrawSecond, permLedgerView, permSymbolView,
	permSymbolEdit, permFeeView, permFeeEdit, permFeeApprove, permRewardView, permRewardEdit,
	permRewardApprove, permCouponApprove, permContentEdit, permAuditView, permAdminManage, permCopyView, permCopyReview, permPartnerView, permPartnerReview, permEarnView, permEarnEdit, permVIPView, permVIPReview, permMMView, permMMEdit, permDashView, permFundAdjust, permFundAdjustApprove}

// rolePerms is the RBAC matrix from prototypes/admin/DESIGN.md §3.
var rolePerms = map[string][]string{
	roleSuper:         allPerms,
	roleOperations:    {permDashView, permVIPView, permVIPReview, permEarnView, permEarnEdit, permCopyView, permCopyReview, permPartnerView, permPartnerReview, permUserView, permUserEdit, permKYCView, permSymbolView, permRewardView, permRewardEdit, permContentEdit},
	roleKYCReviewer:   {permUserView, permKYCView, permKYCApprove},
	roleFinanceFirst:  {permFundAdjust, permDashView, permEarnView, permUserView, permWithdrawView, permWithdrawFirst, permLedgerView, permCouponApprove, permRewardView},
	roleFinanceSecond: {permFundAdjust, permFundAdjustApprove, permDashView, permMMView, permEarnView, permUserView, permFeeView, permWithdrawView, permWithdrawSecond, permLedgerView, permFeeApprove, permCouponApprove, permRewardView, permRewardApprove},
	roleRisk:          {permDashView, permMMView, permEarnView, permCopyView, permPartnerView, permUserView, permUserEdit, permKYCView, permWithdrawView, permLedgerView},
	roleSupport:       {permVIPView, permUserView, permKYCView, permLedgerView},
	roleMarketing:     {permEarnView, permPartnerView, permPartnerReview, permRewardView, permRewardEdit, permContentEdit},
	roleAuditor:       {permDashView, permMMView, permVIPView, permEarnView, permCopyView, permPartnerView, permUserView, permKYCView, permWithdrawView, permLedgerView, permSymbolView, permFeeView, permRewardView, permAuditView},
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
