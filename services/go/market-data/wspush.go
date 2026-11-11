package main

import (
	"encoding/json"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// POST /internal/ws/push {userId, channel, data}：给一个用户的连接推私有帧（事件合约结算）。
// 尽力而为，返回实际送到的连接数。
func (s *Server) hWSPush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID  string          `json:"userId"`
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil || in.UserID == "" || !userChannels[in.Channel] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId/channel 不合法")
		return
	}
	n := s.hub.PushUser(in.UserID, in.Channel, in.Data)
	common.WriteJSON(w, http.StatusOK, map[string]any{"delivered": n})
}

// POST /internal/ws/broadcast {frames:[{channel, data}]}：转发合约服务的公开帧。
// 只收 mark:{SYMBOL}、futures-ticker:{SYMBOL}、futures-kline:{SYMBOL}:{interval}；mark 会再发一份 index:{SYMBOL}。
func (s *Server) hWSBroadcast(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Frames []struct {
			Channel string          `json:"channel"`
			Data    json.RawMessage `json:"data"`
		} `json:"frames"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	n := 0
	for _, f := range in.Frames {
		if relayChannelRe.MatchString(f.Channel) && len(f.Data) > 0 {
			s.hub.Relay(f.Channel, f.Data)
			n++
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"relayed": n})
}
