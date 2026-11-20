package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// GET /im/conversations
//
// Returns the user's conversation list, newest first. Each entry carries the
// unread counter plus a preview of the last message for easy UI rendering.
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	limit := queryLimit(r, 50, 200)
	convs, unread, err := s.store.listConversations(r.Context(), uid, limit)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "会话列表加载失败")
		return
	}
	items := make([]map[string]any, 0, len(convs))
	for i, c := range convs {
		entry := map[string]any{
			"id":             c.ID,
			"conversationId": c.ID,
			"kind":           c.Kind,
			"type":           c.Kind,
			"unread":         unread[i],
			"MsgUnread":      unread[i],
			"updatedAt":      c.UpdatedAt.UnixMilli(),
		}
		if c.LastMsgAt.Valid {
			entry["lastMsgAt"] = c.LastMsgAt.Time.UnixMilli()
			entry["MsgTimeStamp"] = c.LastMsgAt.Time.Unix()
		}
		switch c.Kind {
		case "c2c":
			lo, hi := c.PeerLo.String, c.PeerHi.String
			peer := lo
			if peer == uid {
				peer = hi
			}
			entry["peerId"] = peer
			entry["To_Account"] = peer
		case "group":
			if c.GroupID.Valid {
				g, err := s.store.getGroup(r.Context(), c.GroupID.Int64)
				if err == nil {
					entry["groupId"] = g.ID
					entry["GroupId"] = g.ID
					entry["name"] = g.Name
					entry["Name"] = g.Name
					if g.Avatar.Valid {
						entry["avatar"] = g.Avatar.String
						entry["FaceUrl"] = g.Avatar.String
					}
					entry["memberCount"] = g.MemberCount
				}
			}
		}
		items = append(items, entry)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"list":      items,
		"SessionList": items,
	})
}
