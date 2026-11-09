package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Card product and applications. With no approved issuer the public state stays
// unconfigured, and applying does not insert a row or claim a submission.

func cardDecision(action, actor, updatedBy, submittedBy, status, issuerName, issuerRef string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return errors.New("actor")
	}
	switch action {
	case "save":
		if status == "published" || status == "pending_review" {
			return errors.New("status")
		}
		return nil
	case "submit":
		if status != "draft" && status != "rejected" && status != "unconfigured" {
			return errors.New("status")
		}
		if strings.TrimSpace(issuerName) == "" || strings.TrimSpace(issuerRef) == "" {
			return errors.New("issuer")
		}
	case "approve":
		if status != "pending_review" || strings.TrimSpace(updatedBy) == "" || strings.TrimSpace(submittedBy) == "" || actor == updatedBy || actor == submittedBy {
			return errors.New("self")
		}
		if strings.TrimSpace(issuerName) == "" || strings.TrimSpace(issuerRef) == "" {
			return errors.New("issuer")
		}
	case "reject":
		if status != "pending_review" {
			return errors.New("status")
		}
	default:
		return errors.New("action")
	}
	return nil
}

func cardUnconfigured() map[string]any {
	return map[string]any{
		"configured": false, "submitted": false, "status": "unconfigured",
		"product": nil, "issuer": nil, "application": nil,
	}
}

func (s *Server) cardDB() *sql.DB {
	if s.store == nil {
		return nil
	}
	return s.store.db
}

func (s *Server) publishedCard(r *http.Request) (map[string]any, bool) {
	db := s.cardDB()
	if db == nil {
		return nil, false
	}
	var summary, name, ref string
	err := db.QueryRowContext(r.Context(), `SELECT summary, issuer_legal_name, issuer_registration_ref FROM card_products WHERE id=1 AND status='published'`).Scan(&summary, &name, &ref)
	if err != nil || strings.TrimSpace(name) == "" || strings.TrimSpace(ref) == "" {
		return nil, false
	}
	return map[string]any{
		"configured": true, "submitted": false, "status": "published",
		"product":     map[string]any{"summary": summary, "format": "plain"},
		"issuer":      map[string]any{"legalName": name, "registrationRef": ref},
		"application": nil,
	}, true
}

func (s *Server) handleCardProduct(w http.ResponseWriter, r *http.Request) {
	if view, ok := s.publishedCard(r); ok {
		common.WriteJSON(w, http.StatusOK, view)
		return
	}
	common.WriteJSON(w, http.StatusOK, cardUnconfigured())
}

func (s *Server) handleCardApplication(w http.ResponseWriter, r *http.Request) {
	view := cardUnconfigured()
	if pub, ok := s.publishedCard(r); ok {
		view = pub
	}
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if view["configured"] != true || s.cardDB() == nil {
		common.WriteJSON(w, http.StatusOK, view)
		return
	}
	var st string
	err := s.store.db.QueryRowContext(r.Context(), `SELECT status FROM card_applications WHERE user_id=$1`, u.ID).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		common.WriteJSON(w, http.StatusOK, view)
		return
	}
	view["submitted"] = st == "submitted" || st == "in_review" || st == "approved" || st == "rejected"
	view["application"] = map[string]any{"status": st}
	view["status"] = st
	common.WriteJSON(w, http.StatusOK, view)
}

func (s *Server) handleCardApply(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if _, ok := s.publishedCard(r); !ok {
		body := cardUnconfigured()
		body["code"] = "CARD_UNCONFIGURED"
		body["message"] = "卡片发行方未配置，申请未提交"
		body["requestId"] = common.RequestID()
		common.WriteJSON(w, http.StatusConflict, body)
		return
	}
	var st string
	err := s.store.db.QueryRowContext(r.Context(), `SELECT status FROM card_applications WHERE user_id=$1`, u.ID).Scan(&st)
	if err == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"configured": true, "submitted": true, "status": st, "application": map[string]any{"status": st}})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `INSERT INTO card_applications (user_id, status) VALUES ($1,'submitted')`, u.ID); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"configured": true, "submitted": true, "status": "submitted", "application": map[string]any{"status": "submitted"}})
}

func (s *Server) handleInternalCardGet(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	db := s.cardDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, cardUnconfigured())
		return
	}
	var status, summary, name, ref, updatedBy, submittedBy, reviewedBy string
	err := db.QueryRowContext(r.Context(), `SELECT status, summary, issuer_legal_name, issuer_registration_ref, updated_by, submitted_by, reviewed_by FROM card_products WHERE id=1`).
		Scan(&status, &summary, &name, &ref, &updatedBy, &submittedBy, &reviewedBy)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteJSON(w, http.StatusOK, cardUnconfigured())
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"status": status, "configured": status == "published", "summary": summary,
		"issuerLegalName": name, "issuerRegistrationRef": ref,
		"updatedBy": updatedBy, "submittedBy": submittedBy, "reviewedBy": reviewedBy,
	})
}

func (s *Server) handleInternalCardWrite(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.internalOnly(w, r) {
			return
		}
		db := s.cardDB()
		if db == nil {
			common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
			return
		}
		var req struct {
			Summary    string `json:"summary"`
			IssuerName string `json:"issuerLegalName"`
			IssuerRef  string `json:"issuerRegistrationRef"`
			UpdatedBy  string `json:"updatedBy"`
			Note       string `json:"note"`
		}
		if r.ContentLength != 0 {
			if err := common.DecodeJSON(r, &req); err != nil {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
				return
			}
		}
		var status, updatedBy, submittedBy, name, ref string
		err := db.QueryRowContext(r.Context(), `SELECT status, updated_by, submitted_by, issuer_legal_name, issuer_registration_ref FROM card_products WHERE id=1`).
			Scan(&status, &updatedBy, &submittedBy, &name, &ref)
		if errors.Is(err, sql.ErrNoRows) {
			status = "unconfigured"
		} else if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if action == "save" {
			name, ref = strings.TrimSpace(req.IssuerName), strings.TrimSpace(req.IssuerRef)
		}
		if err := cardDecision(action, req.UpdatedBy, updatedBy, submittedBy, status, name, ref); err != nil {
			common.WriteError(w, http.StatusForbidden, "CARD_REVIEW_DENIED", "发行方资料不足，或不能由同一人批准")
			return
		}
		switch action {
		case "save":
			_, err = db.ExecContext(r.Context(), `INSERT INTO card_products (id, status, summary, issuer_legal_name, issuer_registration_ref, updated_by, updated_at)
				VALUES (1,'draft',$1,$2,$3,$4,now())
				ON CONFLICT (id) DO UPDATE SET status='draft', summary=EXCLUDED.summary, issuer_legal_name=EXCLUDED.issuer_legal_name,
				issuer_registration_ref=EXCLUDED.issuer_registration_ref, updated_by=EXCLUDED.updated_by, updated_at=now()`,
				strings.TrimSpace(req.Summary), name, ref, strings.TrimSpace(req.UpdatedBy))
		case "submit":
			_, err = db.ExecContext(r.Context(), `UPDATE card_products SET status='pending_review', submitted_by=$2, updated_at=now() WHERE id=1`, 1, strings.TrimSpace(req.UpdatedBy))
		case "approve":
			_, err = db.ExecContext(r.Context(), `UPDATE card_products SET status='published', reviewed_by=$2, review_note=$3, updated_at=now() WHERE id=1`, 1, strings.TrimSpace(req.UpdatedBy), strings.TrimSpace(req.Note))
		default:
			_, err = db.ExecContext(r.Context(), `UPDATE card_products SET status='rejected', reviewed_by=$2, review_note=$3, updated_at=now() WHERE id=1`, 1, strings.TrimSpace(req.UpdatedBy), strings.TrimSpace(req.Note))
		}
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		s.handleInternalCardGet(w, r)
	}
}
