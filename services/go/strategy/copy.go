package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// One-click copy from the strategy square: the copier's new bot reuses the
// source bot's symbol, mode, price range and grid count; the copier chooses
// the investment and optional stop-loss / take-profit. Each successful copy
// bumps the source bot's copy_count, which the square shows as copyCount.

type copyReq struct {
	Investment string  `json:"investment"`
	StopLoss   *string `json:"stopLossPrice"`
	TakeProfit *string `json:"takeProfitPrice"`
	SellOnStop *bool   `json:"sellBaseOnStop"`
}

func (s *Server) hCopy(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "策略不存在")
		return
	}
	var req copyReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
		return
	}
	src, err := s.bot(r.Context(), id)
	// Only bots that are listed on the square can be copied.
	if err != nil || src.Status != "running" || time.Since(src.CreatedAt) < s.cfg.SquareMinRun {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "策略不存在或已停止")
		return
	}
	if src.UserID == uid {
		common.WriteError(w, http.StatusBadRequest, "SELF_COPY", "不能复制自己的策略")
		return
	}
	b, err := s.Create(r.Context(), uid, createReq{
		Symbol: src.Symbol, Mode: src.Mode, Lower: src.Lower.String(), Upper: src.Upper.String(), Grids: src.Grids,
		Investment: req.Investment, StopLoss: req.StopLoss, TakeProfit: req.TakeProfit, SellOnStop: req.SellOnStop,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.recordCopy(r.Context(), src.ID, b.ID); err != nil {
		writeErr(w, err)
		return
	}
	v := s.view(r.Context(), b, true)
	v["copiedFrom"] = strconv.FormatInt(src.ID, 10)
	common.WriteJSON(w, http.StatusCreated, v)
}

func (s *Server) recordCopy(ctx context.Context, srcID, newID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE grid_bots SET copied_from=$1 WHERE id=$2`, srcID, newID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE grid_bots SET copy_count=copy_count+1 WHERE id=$1`, srcID); err != nil {
		return err
	}
	return tx.Commit()
}

// copyCounts returns copy_count for the given bot ids.
func (s *Server) copyCounts(ctx context.Context, ids []int64) map[int64]int {
	out := map[int64]int{}
	if len(ids) == 0 {
		return out
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, copy_count FROM grid_bots WHERE id = ANY($1::bigint[])`, int64Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

func int64Array(ids []int64) string {
	b := []byte{'{'}
	for i, id := range ids {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, id, 10)
	}
	return string(append(b, '}'))
}
