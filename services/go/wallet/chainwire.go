package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/wallet/chainkit"
	"github.com/shopspring/decimal"
)

// chainwire.go wires the real-chain adapter into the server: startup, the
// background worker (sync -> scan -> withdrawals -> sweeps) and admin endpoints.

// chainMode returns WALLET_CHAIN (mock | testnet | mainnet).
func chainMode() string {
	m := strings.ToLower(common.Env("WALLET_CHAIN", "mock"))
	switch m {
	case "mock", "testnet", "mainnet":
		return m
	}
	log.Fatalf("WALLET_CHAIN must be mock, testnet or mainnet (got %q)", m)
	return ""
}

// UseRealChain replaces the mock chain with a RealChain talking to the hot
// signer at WALLET_SIGNER_URL. It waits up to a minute for the signer.
func (s *Server) UseRealChain(ctx context.Context, mode string) error {
	url := common.Env("WALLET_SIGNER_URL", "")
	token := common.Env("WALLET_SIGNER_TOKEN", "")
	if url == "" || token == "" {
		return errors.New("WALLET_SIGNER_URL and WALLET_SIGNER_TOKEN are required for a real chain")
	}
	signer := chainkit.NewRemoteHotSigner(url, token)
	var rc *RealChain
	var err error
	for i := 0; i < 30; i++ {
		if rc, err = NewRealChain(ctx, mode, s.store, s.cfg.Networks, signer); err == nil {
			break
		}
		if !isTransient(err) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return err
	}
	s.chain, s.real, s.mock = rc, rc, nil
	return nil
}

// isTransient reports network-level failures where the remote side may or may
// not have acted (timeouts, connection resets) as opposed to explicit refusals.
func isTransient(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		strings.Contains(err.Error(), "connection refused")
}

// sendRealWithdrawal claims the withdrawal (approved -> processing, no hash)
// before sending so it can never be paid twice, then records the tx hash.
func (s *Server) sendRealWithdrawal(ctx context.Context, w *Withdrawal, n Network, sender ChainSender) bool {
	claimed, err := s.store.ClaimForBroadcast(ctx, w.ID)
	if err != nil || !claimed {
		return false
	}
	hash, err := sender.SendWithdrawal(ctx, n, w.Address, sendAmount(w))
	if err != nil {
		if isTransient(err) && !strings.Contains(err.Error(), "connection refused") {
			log.Printf("withdrawal %d: send outcome unknown (%v); left processing for manual reconciliation", w.ID, err)
			return false
		}
		log.Printf("withdrawal %d: send failed: %v", w.ID, err)
		s.failAndRelease(ctx, w, "broadcast_failed")
		return false
	}
	if err := s.store.SetWithdrawalTx(ctx, w.ID, hash); err != nil {
		log.Printf("withdrawal %d: sent tx %s but could not record it: %v", w.ID, hash, err)
		return false
	}
	log.Printf("withdrawal %d broadcast tx=%s", w.ID, hash)
	return true
}

func (s *Server) sweepMin(asset, network string) decimal.Decimal {
	if v := common.Env(envKey("WALLET_SWEEP_MIN", asset, network), ""); v != "" {
		if d, err := decimal.NewFromString(v); err == nil {
			return d
		}
	}
	if n, ok := s.cfg.Networks.Lookup(asset, network); ok {
		return n.MinDeposit
	}
	return decimal.Zero
}

// RunWorker runs sync, scan, withdrawal processing and sweeps every interval.
func (s *Server) RunWorker(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.workerPass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) workerPass(ctx context.Context) {
	pass, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := s.store.LoadNetworkOverrides(pass); err != nil {
		log.Printf("worker: network overrides: %v", err)
	}
	if s.real != nil {
		if n, _ := s.real.Sync(pass); n > 0 {
			log.Printf("worker: %d new on-chain deposits", n)
		}
	}
	s.retryPendingITX(pass)
	if _, err := s.scanner.ScanAll(pass); err != nil {
		log.Printf("worker: scan: %v", err)
	}
	if _, _, err := s.ProcessWithdrawals(pass); err != nil {
		log.Printf("worker: withdrawals: %v", err)
	}
	if s.real != nil && os.Getenv("WALLET_SWEEP_ENABLED") != "false" {
		if _, err := s.real.Sweep(pass, s.sweepMin); err != nil {
			log.Printf("worker: sweep: %v", err)
		}
	}
}

func (s *Server) handleChainSync(w http.ResponseWriter, r *http.Request) {
	n, err := s.real.Sync(r.Context())
	resp := map[string]interface{}{"recorded": n}
	if err != nil {
		resp["errors"] = err.Error()
	}
	common.WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) handleChainSweep(w http.ResponseWriter, r *http.Request) {
	n, err := s.real.Sweep(r.Context(), s.sweepMin)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]int{"started": n})
}

func (s *Server) handleHotWallets(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"mode": s.real.Kind(), "items": s.real.HotWallets(r.Context())})
}

func (s *Server) handleSweeps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.RecentSweeps(r.Context(), 100)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	items := make([]map[string]string, 0, len(rows))
	for _, sw := range rows {
		items = append(items, map[string]string{
			"id": itoa64(sw.ID), "network": sw.Network, "asset": sw.Asset, "address": sw.Address,
			"stage": sw.Stage, "amount": sw.Amount.String(), "destination": sw.Destination,
			"topupTx": sw.TopupTx, "sweepTx": sw.SweepTx, "updatedAt": sw.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}
