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
	permSupportView    = "support:view"
	permSupportReply   = "support:reply"
	permMMView         = "mm:view"
	permMMEdit         = "mm:edit"
	permDashView       = "dash:view"
	// 人工调账: create/list, and the second-person approval (maker-checker).
	permFundAdjust        = "fund:adjust"
	permFundAdjustApprove = "fund:adjust:approve"
	// 热转冷手动归集：仅超级管理员默认拥有，并列入 lockedPerms。
	permWalletColdSweep = "wallet:cold_sweep"
	// 风控规则：查看命中/规则；编辑规则与处理命中（只告警，不自动冻结或拦截资金）。
	permRiskView = "risk:view"
	permRiskEdit = "risk:edit"
)

var allPerms = []string{permUserView, permUserEdit, permUserPII, permKYCView, permKYCApprove,
	permWithdrawView, permWithdrawFirst, permWithdrawSecond, permLedgerView, permSymbolView,
	permSymbolEdit, permFeeView, permFeeEdit, permFeeApprove, permRewardView, permRewardEdit,
	permRewardApprove, permCouponApprove, permContentEdit, permAuditView, permAdminManage, permCopyView, permCopyReview, permPartnerView, permPartnerReview, permEarnView, permEarnEdit, permVIPView, permVIPReview, permMMView, permMMEdit, permDashView, permFundAdjust, permFundAdjustApprove, permSupportView, permSupportReply, permWalletColdSweep, permRiskView, permRiskEdit,
	permBizView, permBizEdit, permConfigApprove, permStrategyForce}

// rolePerms is the RBAC matrix from prototypes/admin/DESIGN.md §3.
var rolePerms = map[string][]string{
	roleSuper:         allPerms,
	roleOperations:    {permDashView, permSupportView, permSupportReply, permVIPView, permVIPReview, permEarnView, permEarnEdit, permCopyView, permCopyReview, permPartnerView, permPartnerReview, permUserView, permUserEdit, permKYCView, permSymbolView, permRewardView, permRewardEdit, permContentEdit, permBizView, permBizEdit},
	roleKYCReviewer:   {permUserView, permKYCView, permKYCApprove},
	roleFinanceFirst:  {permFundAdjust, permDashView, permEarnView, permUserView, permWithdrawView, permWithdrawFirst, permLedgerView, permCouponApprove, permRewardView, permBizView},
	roleFinanceSecond: {permFundAdjust, permFundAdjustApprove, permDashView, permMMView, permEarnView, permUserView, permFeeView, permWithdrawView, permWithdrawSecond, permLedgerView, permFeeApprove, permCouponApprove, permRewardView, permRewardApprove, permBizView, permConfigApprove},
	roleRisk:          {permRiskView, permRiskEdit, permDashView, permMMView, permEarnView, permCopyView, permPartnerView, permUserView, permUserEdit, permKYCView, permWithdrawView, permLedgerView, permBizView, permStrategyForce},
	roleSupport:       {permSupportView, permSupportReply, permVIPView, permUserView, permKYCView, permLedgerView},
	roleMarketing:     {permEarnView, permPartnerView, permPartnerReview, permRewardView, permRewardEdit, permContentEdit, permBizView},
	roleAuditor:       {permRiskView, permDashView, permSupportView, permMMView, permVIPView, permEarnView, permCopyView, permPartnerView, permUserView, permKYCView, permWithdrawView, permLedgerView, permSymbolView, permFeeView, permRewardView, permAuditView, permBizView},
}

func validRole(r string) bool { _, ok := effectiveRoles()[r]; return ok }

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func can(roles []string, perm string) bool {
	eff := effectiveRoles()
	for _, r := range roles {
		for _, p := range eff[r] {
			if p == perm {
				return true
			}
		}
	}
	return false
}
