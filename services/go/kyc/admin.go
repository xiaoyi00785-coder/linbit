package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// adminApplicationDTO is the admin view of an application.
type adminApplicationDTO struct {
	ID          string        `json:"id"`
	UserID      string        `json:"userId"`
	Level       int           `json:"level"`
	Status      string        `json:"status"`
	FirstName   string        `json:"firstName"`
	LastName    string        `json:"lastName"`
	Country     string        `json:"country"`
	IDType      string        `json:"idType"`
	IDNumber    string        `json:"idNumber"`
	DateOfBirth string        `json:"dateOfBirth"`
	Reason      *string       `json:"reason"`
	SubmittedAt string        `json:"submittedAt"`
	ReviewedAt  *string       `json:"reviewedAt"`
	Documents   []adminDocDTO `json:"documents,omitempty"`
}

// adminDocDTO points the reviewer at a controlled document-read URL.
type adminDocDTO struct {
	Side   string `json:"side"`
	FileID string `json:"fileId"`
	URL    string `json:"url"`
}

func toAdminDTO(a *Application, withDocs bool) adminApplicationDTO {
	dto := adminApplicationDTO{
		ID: appID(a.ID), UserID: a.UserID, Level: a.Level, Status: a.Status,
		FirstName: a.FirstName, LastName: a.LastName, Country: a.Country,
		IDType: a.IDType, IDNumber: a.IDNumber,
		DateOfBirth: a.DateOfBirth.UTC().Format("2006-01-02"),
		SubmittedAt: a.SubmittedAt.UTC().Format(time.RFC3339),
	}
	if a.Reason.Valid {
		dto.Reason = strptr(a.Reason.String)
	}
	if a.ReviewedAt.Valid {
		dto.ReviewedAt = strptr(a.ReviewedAt.Time.UTC().Format(time.RFC3339))
	}
	if withDocs {
		dto.Documents = docRefs(a)
	}
	return dto
}

// docRefs builds the controlled-read references for an application's documents.
func docRefs(a *Application) []adminDocDTO {
	var out []adminDocDTO
	add := func(side string, id int64, valid bool) {
		if valid {
			out = append(out, adminDocDTO{Side: side, FileID: docFileID(id),
				URL: "/admin/kyc/documents/" + docFileID(id)})
		}
	}
	add(sideFront, a.FrontDocID.Int64, a.FrontDocID.Valid)
	add(sideSelfie, a.SelfieDocID.Int64, a.SelfieDocID.Valid)
	add(sideBack, a.BackDocID.Int64, a.BackDocID.Valid)
	add(sideProofOfAddress, a.ProofDocID.Int64, a.ProofDocID.Valid)
	return out
}

// handleAdminList returns applications, optionally filtered by ?status=.
//
//	GET /admin/kyc/applications?status=
func (s *Server) handleAdminList(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	switch status {
	case "", statusPending, statusApproved, statusRejected:
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 不合法")
		return
	}
	rows, err := s.store.ListApplications(r.Context(), status, 200)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]adminApplicationDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toAdminDTO(&rows[i], false))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleAdminGet returns one application with controlled document references.
//
//	GET /admin/kyc/applications/{id}
func (s *Server) handleAdminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAppID(r.PathValue("id"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的申请编号")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "申请不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, toAdminDTO(app, true))
}

// handleAdminApprove approves a pending application and syncs the user's kycLevel
// to the account service.
//
//	POST /admin/kyc/applications/{id}/approve
func (s *Server) handleAdminApprove(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAppID(r.PathValue("id"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的申请编号")
		return
	}
	applied, err := s.store.Approve(r.Context(), id)
	if err == ErrIDNumberTaken {
		common.WriteError(w, http.StatusConflict, "ID_NUMBER_TAKEN", "该证件已被其他账户认证")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该申请不在待审核状态")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	// Sync the entitlement to the account service (account never downgrades). The
	// sync is persisted first (kyc_level_sync) and attempted immediately; a failure
	// does not roll back the approval and is retried by the background worker with
	// exponential backoff.
	if err := s.store.EnqueueLevelSync(r.Context(), app.UserID, app.Level); err != nil {
		log.Printf("approve %d: enqueue kyc-level sync: %v", id, err)
	} else {
		s.trySync(r.Context(), app.UserID, app.Level, 0)
	}
	common.Notify(common.Notice{UID: app.UserID, Kind: common.NoticeSecurity, Title: "身份认证已通过",
		Body: "您的身份认证（Lv" + strconv.Itoa(app.Level) + "）已通过人工审核。"})
	common.WriteJSON(w, http.StatusOK, toAdminDTO(app, false))
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

// handleAdminReject rejects a pending application; a reason is required.
//
//	POST /admin/kyc/applications/{id}/reject
func (s *Server) handleAdminReject(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAppID(r.PathValue("id"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的申请编号")
		return
	}
	var req rejectRequest
	_ = common.DecodeJSON(r, &req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "驳回原因必填")
		return
	}
	applied, err := s.store.Reject(r.Context(), id, reason)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该申请不在待审核状态")
		return
	}
	app, _ := s.store.GetApplication(r.Context(), id)
	if app != nil {
		common.Notify(common.Notice{UID: app.UserID, Kind: common.NoticeSecurity, Title: "身份认证未通过",
			Body: "您的身份认证申请未通过人工审核，原因：" + reason + "。请修改后重新提交。"})
	}
	common.WriteJSON(w, http.StatusOK, toAdminDTO(app, false))
}

// appID renders an application id as "app_<n>".
func appID(id int64) string { return "app_" + strconv.FormatInt(id, 10) }

// parseAppID accepts either a raw numeric id or the "app_<n>" form.
func parseAppID(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "app_")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
