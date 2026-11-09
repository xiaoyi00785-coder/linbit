package common

// usercredit.go：管理员给普通用户资金账户记虚拟测试网资金的开关。只允许测试网。

import (
	"os"
	"strings"
)

// UserCreditAllowed 判断能否做 admin_testnet_credit。硬限制只允许测试网：
//   - WALLET_CHAIN 必须正好是 "testnet"（mock、mainnet、未知都拒绝）；
//   - ADMIN_USER_CREDIT_ENABLED 必须是 "true"；
//   - 任一主网信号都拒绝，即使开关开着：LINBIT_NETWORK、NETWORK 或 APP_ENV = mainnet，
//     或 LINBIT_MAINNET / MAINNET = true。
//
// 返回的原因不含密钥。
func UserCreditAllowed() (bool, string) {
	low := func(k string) string { return strings.ToLower(strings.TrimSpace(os.Getenv(k))) }
	for _, k := range []string{"LINBIT_NETWORK", "NETWORK", "APP_ENV"} {
		if low(k) == "mainnet" {
			return false, k + "=mainnet: user credit is forbidden on mainnet"
		}
	}
	for _, k := range []string{"LINBIT_MAINNET", "MAINNET"} {
		if v := low(k); v == "true" || v == "1" {
			return false, k + "=true: user credit is forbidden on mainnet"
		}
	}
	if c := low("WALLET_CHAIN"); c != "testnet" {
		if c == "" {
			c = "(unset)"
		}
		return false, "WALLET_CHAIN=" + c + " is not testnet: user credit is testnet-only"
	}
	if low("ADMIN_USER_CREDIT_ENABLED") != "true" {
		return false, "ADMIN_USER_CREDIT_ENABLED is not true"
	}
	return true, "WALLET_CHAIN=testnet, ADMIN_USER_CREDIT_ENABLED=true (virtual, unbacked)"
}
