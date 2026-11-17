package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/linbit/platform/services/go/common"
)

type chatFolder struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (c *Chat) hJoined(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	uid := r.Header.Get("X-User-Id")
	ctx := r.Context()
	folders, err := c.folders(ctx, uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "分组加载失败")
		return
	}
	rows, err := c.srv.store.db.QueryContext(ctx, `SELECT room, folder_id FROM chat_members WHERE uid=$1 ORDER BY created_at DESC`, uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "群聊加载失败")
		return
	}
	defer rows.Close()
	type membership struct {
		room   string
		folder *int64
	}
	var members []membership
	for rows.Next() {
		var m membership
		var folderID *int64
		if err := rows.Scan(&m.room, &folderID); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "群聊加载失败")
			return
		}
		m.folder = folderID
		members = append(members, m)
	}
	byID := map[string]chatRoom{}
	for _, room := range c.rooms(ctx) {
		byID[room.ID] = room
	}
	out := make([]chatRoom, 0, len(members))
	for _, m := range members {
		room, ok := byID[m.room]
		if !ok {
			continue
		}
		if m.folder != nil {
			room.FolderID = m.folder
		}
		out = append(out, room)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"rooms": out, "folders": folders})
}

func (c *Chat) hFolders(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	folders, err := c.folders(r.Context(), r.Header.Get("X-User-Id"))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "分组加载失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (c *Chat) folders(ctx context.Context, uid string) ([]chatFolder, error) {
	rows, err := c.srv.store.db.QueryContext(ctx, `SELECT id, name FROM chat_folders WHERE uid=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chatFolder{}
	for rows.Next() {
		var f chatFolder
		if err := rows.Scan(&f.ID, &f.Name); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (c *Chat) hCreateFolder(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 20 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "分组名须为 1-20 个字")
		return
	}
	var f chatFolder
	err := c.srv.store.db.QueryRowContext(r.Context(), `INSERT INTO chat_folders (uid, name) VALUES ($1,$2) RETURNING id, name`, r.Header.Get("X-User-Id"), name).Scan(&f.ID, &f.Name)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			common.WriteError(w, http.StatusConflict, "FOLDER_EXISTS", "已有同名分组")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "创建分组失败")
		return
	}
	common.WriteJSON(w, http.StatusCreated, f)
}

func (c *Chat) hJoin(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	room := r.PathValue("room")
	if !c.roomExists(r.Context(), room) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "聊天室不存在")
		return
	}
	var in struct {
		FolderID *int64 `json:"folderId"`
	}
	if r.ContentLength > 0 {
		if err := common.DecodeJSON(r, &in); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
	}
	uid := r.Header.Get("X-User-Id")
	if in.FolderID != nil {
		var owner string
		err := c.srv.store.db.QueryRowContext(r.Context(), `SELECT uid FROM chat_folders WHERE id=$1`, *in.FolderID).Scan(&owner)
		if err != nil || owner != uid {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "分组不存在")
			return
		}
	}
	_, err := c.srv.store.db.ExecContext(r.Context(), `INSERT INTO chat_members (uid, room, folder_id) VALUES ($1,$2,$3)
		ON CONFLICT (uid, room) DO UPDATE SET folder_id=EXCLUDED.folder_id`, uid, room, in.FolderID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "加入失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
}

func (c *Chat) hLeave(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	_, err := c.srv.store.db.ExecContext(r.Context(), `DELETE FROM chat_members WHERE uid=$1 AND room=$2`, r.Header.Get("X-User-Id"), r.PathValue("room"))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "退出失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
