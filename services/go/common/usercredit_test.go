package common

import "testing"

func TestUserCreditAllowed(t *testing.T) {
	keys := []string{"LINBIT_NETWORK", "NETWORK", "APP_ENV", "LINBIT_MAINNET", "MAINNET", "WALLET_CHAIN", "ADMIN_USER_CREDIT_ENABLED", "LINBIT_ENV"}
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true"}, true},
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "LINBIT_ENV": "production"}, true},
		{map[string]string{"WALLET_CHAIN": "testnet"}, false},
		{map[string]string{"WALLET_CHAIN": "mock", "ADMIN_USER_CREDIT_ENABLED": "true"}, false},
		{map[string]string{"WALLET_CHAIN": "mainnet", "ADMIN_USER_CREDIT_ENABLED": "true"}, false},
		{map[string]string{"ADMIN_USER_CREDIT_ENABLED": "true"}, false},
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "APP_ENV": "mainnet"}, false},
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "NETWORK": "Mainnet"}, false},
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "LINBIT_NETWORK": "mainnet"}, false},
		{map[string]string{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "LINBIT_MAINNET": "true"}, false},
	}
	for i, c := range cases {
		for _, k := range keys {
			t.Setenv(k, c.env[k])
		}
		if got, why := UserCreditAllowed(); got != c.want {
			t.Errorf("case %d %v: got %v (%s)", i, c.env, got, why)
		}
	}
}
