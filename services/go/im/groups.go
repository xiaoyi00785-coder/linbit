package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// POST /im/groups
//
// Creates a group with the caller as owner. Optional initial member list is
// added in the same request so a client creating "a new chat with Alice and
// Bob" issues one POST.
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	var body struct {
		Name       string   `json:"name"`
		Avatar     string   `json:"avatar"`
		FaceUrl    string   `json:"faceUrl"`
		MaxMembers int      `json:"maxMembers"`
		Members    []string `json:"members"`
		MemberList []string `json:"memberList"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if body.Name == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "群名不能为空")
		return
	}
	if len(body.Name) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "群名过长")
		return
	}
	avatar := body.Avatar
	if avatar == "" {
		avatar = body.FaceUrl
	}
	maxMembers := body.MaxMembers
	if maxMembers <= 0 || maxMembers > 2000 {
		maxMembers = 500
	}
	g, err := s.store.createGroup(r.Context(), body.Name, uid, avatar, maxMembers)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "建群失败")
		return
	}
	members := body.Members
	if len(members) == 0 {
		members = body.MemberList
	}
	for _, m := range members {
		if m == "" || m == uid {
			continue
		}
		if _, err := s.store.addGroupMember(r.Context(), g.ID, m, "member"); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "添加成员失败")
			return
		}
	}
	g, _ = s.store.getGroup(r.Context(), g.ID)
	common.WriteJSON(w, http.StatusOK, groupDTO(g))
}

// GET /im/groups/{id}
func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	g, err := s.store.getGroup(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	// Non-members get the public summary only (name + member count), never
	// the announcement body.
	if _, err := s.store.getGroupMember(r.Context(), id, uid); err != nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{
			"id":          g.ID,
			"GroupId":     g.ID,
			"name":        g.Name,
			"memberCount": g.MemberCount,
			"public":      true,
		})
		return
	}
	common.WriteJSON(w, http.StatusOK, groupDTO(g))
}

// GET /im/groups/{id}/members
func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	if _, err := s.store.getGroupMember(r.Context(), id, uid); err != nil {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
		return
	}
	limit := queryLimit(r, 100, 500)
	ms, err := s.store.listGroupMembers(r.Context(), id, limit)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "成员加载失败")
		return
	}
	items := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		entry := map[string]any{
			"userId":    m.UID,
			"Member_Account": m.UID,
			"role":      m.Role,
			"Role":      m.Role,
			"joinedAt":  m.JoinedAt.UnixMilli(),
		}
		if m.MutedUntil.Valid {
			entry["mutedUntil"] = m.MutedUntil.Time.UnixMilli()
			entry["MuteUntil"] = m.MutedUntil.Time.Unix()
		}
		if m.Nickname.Valid {
			entry["nickname"] = m.Nickname.String
		}
		items = append(items, entry)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "MemberList": items})
}

// POST /im/groups/{id}/members {uids:[...]}
func (s *Server) handleAddMembers(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	if err := s.requireAdmin(r, id, uid); err != nil {
		writeAdminErr(w, err)
		return
	}
	g, err := s.store.getGroup(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	var body struct {
		UIDs []string `json:"uids"`
	}
	if err := common.DecodeJSON(r, &body); err != nil || len(body.UIDs) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uids")
		return
	}
	added := 0
	for _, u := range body.UIDs {
		if u == "" {
			continue
		}
		if g.MemberCount+added >= g.MaxMembers {
			common.WriteError(w, http.StatusBadRequest, "GROUP_FULL", "群成员已满")
			return
		}
		ok, err := s.store.addGroupMember(r.Context(), id, u, "member")
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "添加失败")
			return
		}
		if ok {
			added++
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"added": added})
}

// DELETE /im/groups/{id}/members/{uid}
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	target := r.PathValue("uid")
	if target == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uid")
		return
	}
	// A member may leave (DELETE their own row); otherwise admin privileges required.
	if target != uid {
		if err := s.requireAdmin(r, id, uid); err != nil {
			writeAdminErr(w, err)
			return
		}
	}
	g, err := s.store.getGroup(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	if target == g.OwnerUID {
		common.WriteError(w, http.StatusBadRequest, "OWNER_LOCKED", "群主无法被移除")
		return
	}
	removed, err := s.store.removeGroupMember(r.Context(), id, target)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "移除失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// POST /im/groups/{id}/members/{uid}/role {role}
func (s *Server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	caller := s.uid(r)
	g, err := s.store.getGroup(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	// Role changes require owner privileges (admins cannot promote peers).
	if caller != g.OwnerUID {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "仅群主可调整角色")
		return
	}
	target := r.PathValue("uid")
	var body struct {
		Role string `json:"role"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if body.Role != "admin" && body.Role != "member" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "角色只能为 admin 或 member")
		return
	}
	if target == g.OwnerUID {
		common.WriteError(w, http.StatusBadRequest, "OWNER_LOCKED", "群主角色固定")
		return
	}
	if err := s.store.setRole(r.Context(), id, target, body.Role); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "角色更新失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /im/groups/{id}/mute {muted}
func (s *Server) handleGroupMute(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	if err := s.requireAdmin(r, id, uid); err != nil {
		writeAdminErr(w, err)
		return
	}
	var body struct {
		Muted bool `json:"muted"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if err := s.store.setGroupMuteAll(r.Context(), id, body.Muted); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "禁言设置失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "muted": body.Muted})
}

// POST /im/groups/{id}/members/{uid}/mute {seconds}
func (s *Server) handleMemberMute(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	caller := s.uid(r)
	if err := s.requireAdmin(r, id, caller); err != nil {
		writeAdminErr(w, err)
		return
	}
	target := r.PathValue("uid")
	var body struct {
		Seconds int `json:"seconds"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	var until time.Time
	if body.Seconds > 0 {
		until = s.now().Add(time.Duration(body.Seconds) * time.Second)
	}
	if err := s.store.setMemberMute(r.Context(), id, target, until); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "成员禁言失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PUT /im/groups/{id}/announcement {text}
func (s *Server) handleAnnouncement(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	if err := s.requireAdmin(r, id, uid); err != nil {
		writeAdminErr(w, err)
		return
	}
	var body struct {
		Text         string `json:"text"`
		Announcement string `json:"announcement"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	text := body.Text
	if text == "" {
		text = body.Announcement
	}
	if len(text) > 4096 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "公告过长")
		return
	}
	if err := s.store.setAnnouncement(r.Context(), id, uid, text); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "公告更新失败")
		return
	}
	convID, _ := s.store.groupConvID(r.Context(), id)
	if convID > 0 {
		s.hub.broadcastConversation(convID, map[string]any{
			"event":   "announcement",
			"groupId": id,
			"GroupId": id,
			"text":    text,
			"ts":      s.nowUnixMilli(),
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- helpers ----

// requireAdmin checks the caller is owner or admin of the group.
func (s *Server) requireAdmin(r *http.Request, groupID int64, uid string) error {
	m, err := s.store.getGroupMember(r.Context(), groupID, uid)
	if err != nil {
		return errNotMember
	}
	if m.Role == "owner" || m.Role == "admin" {
		return nil
	}
	return errNotAdmin
}

var (
	errNotMember = errors.New("not a group member")
	errNotAdmin  = errors.New("not a group admin")
)

func writeAdminErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotMember):
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
	case errors.Is(err, errNotAdmin):
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "需要管理员权限")
	default:
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "权限校验失败")
	}
}

// groupDTO renders a group row as the JSON object every group endpoint returns.
func groupDTO(g *Group) map[string]any {
	out := map[string]any{
		"id":           g.ID,
		"GroupId":      g.ID,
		"name":         g.Name,
		"Name":         g.Name,
		"owner":        g.OwnerUID,
		"Owner_Account": g.OwnerUID,
		"maxMembers":   g.MaxMembers,
		"memberCount":  g.MemberCount,
		"MemberNum":    g.MemberCount,
		"muteAll":      g.MuteAll,
		"announcement": g.Announcement,
		"Notification": g.Announcement,
		"createdAt":    g.CreatedAt.UnixMilli(),
	}
	if g.Avatar.Valid {
		out["avatar"] = g.Avatar.String
		out["FaceUrl"] = g.Avatar.String
	}
	if g.AnnouncementAt.Valid {
		out["announcementAt"] = g.AnnouncementAt.Time.UnixMilli()
	}
	if g.ConversationID.Valid {
		out["conversationId"] = g.ConversationID.Int64
	}
	return out
}
