package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/linbit/platform/services/go/common"
)

// GET /home/newcomer，首页上的新人任务条。经 gateway 公开，登录可选。
// 未登录返回模板：每项 locked，cta=login。已登录返回本人进度。
// 没有配置任务，或查进度失败，则退成 {available:false, tasks:[]} 或模板，不把首页打挂。
func (s *Server) handleHomeNewcomer(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-Id")
	rules := s.cfg.Newcomer
	out := map[string]any{"available": len(rules.Tasks) > 0, "loggedIn": uid != "", "enrolled": false,
		"total": rules.total().String(), "asset": s.cfg.PayoutAsset, "days": rules.Days,
		"completed": 0, "claimable": "0", "deadline": nil, "link": "/rewards/newcomer"}
	if len(rules.Tasks) == 0 {
		out["tasks"], out["cta"] = []homeTask{}, "none"
		common.WriteJSON(w, http.StatusOK, out)
		return
	}
	tasks := make([]homeTask, 0, len(rules.Tasks))
	for _, t := range rules.Tasks {
		tasks = append(tasks, homeTask{Key: t.Key, Title: t.Title, Type: t.Type, Kind: t.Kind,
			Amount: t.Amount.String(), Target: t.Target.String(), Progress: "0", Status: "locked"})
	}
	cta := "login"
	if uid != "" {
		st, views, err := s.newcomerState(r.Context(), uid)
		if err != nil {
			log.Printf("home newcomer %s: %v", uid, err)
			out["degraded"] = true
			cta = "open"
		} else {
			tasks = tasks[:0]
			done, claimable, claimed, expired := 0, 0, 0, 0
			for _, v := range views {
				tasks = append(tasks, homeTask{Key: v.Key, Title: v.Title, Type: v.Type, Kind: v.Kind,
					Amount: v.Amount.String(), Target: v.Target.String(), Progress: v.Progress, Status: v.Status})
				switch v.Status {
				case "claimable":
					claimable++
					done++
				case "claimed":
					claimed++
					done++
				case "expired":
					expired++
				}
			}
			out["enrolled"], out["completed"], out["claimable"] = st["enrolled"], done, strconv.Itoa(claimable)
			if d, ok := st["deadline"]; ok {
				out["deadline"] = d
			}
			switch {
			case st["kycOk"] != true:
				cta = "kyc"
			case st["enrolled"] != true:
				cta = "enroll"
			case claimable > 0:
				cta = "claim"
			case claimed+expired == len(views):
				cta, out["available"] = "done", false
			default:
				cta = "continue"
			}
		}
	}
	out["tasks"], out["cta"] = tasks, cta
	common.WriteJSON(w, http.StatusOK, out)
}

type homeTask struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Kind     string `json:"kind"`
	Amount   string `json:"amount"`
	Target   string `json:"target"`
	Progress string `json:"progress"`
	Status   string `json:"status"` // locked | in_progress | claimable | claimed | expired
}
