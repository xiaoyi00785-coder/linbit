package main

import (
	"encoding/json"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// POST /internal/ws/push {userId, channel, data}: private frame to one user's
// sockets (event contract settlements). Best effort; answers how many sockets
// received it.
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

// POST /internal/ws/broadcast {frames:[{channel, data}]}: public frames relayed
// from the futures service (only mark:{SYMBOL}, futures-ticker:{SYMBOL} and
// futures-kline:{SYMBOL}:{interval} are accepted; mark is re-emitted as index:{SYMBOL}).
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
