package chainkit

import "testing"

// Mainnet cold addresses supplied by ops (public addresses, not secrets).
func TestMainnetColdAddressesValidate(t *testing.T) {
	cases := []struct {
		c    Chain
		addr string
	}{
		{ChainBTC, "bc1qqkalddfcrcwc7udq5zxpmwfq5gjn4wj256szyw"},
		{ChainEVM, "0x12c4233FfB0111310D613fbe1e2901D58a09B448"},
		{ChainTron, "TMVQ5Yw8fWSNfPf6aSjqvmXfgeuxFHC6DG"},
		{ChainSol, "3t56c3HcZB8qnn1db3TmCH96g6sgxkNgPr5CRK8um4ar"},
	}
	for _, tc := range cases {
		if err := ValidateAddress(tc.c, tc.addr, "bc"); err != nil {
			t.Errorf("%s %s: %v", tc.c, tc.addr, err)
		}
	}
}
