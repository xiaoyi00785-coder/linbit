package common

import (
	"os"
	"strings"
)

// UserCreditAllowed gates the admin testnet credit to a normal user's funding
// account (admin_testnet_credit). It is HARD testnet-only:
//   - WALLET_CHAIN must be exactly "testnet" (mock/mainnet/unknown refused);
//   - ADMIN_USER_CREDIT_ENABLED must be "true";
//   - any mainnet signal refuses even when the flag is set: LINBIT_NETWORK,
//     NETWORK or APP_ENV = mainnet, or LINBIT_MAINNET/MAINNET = true.
//
// The reason never contains secret values.
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
