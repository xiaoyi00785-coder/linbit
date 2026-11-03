package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// validIDTypes are the accepted identity document types (matches the web UI and
// the openapi KycSubmission enum).
var validIDTypes = map[string]bool{"id_card": true, "passport": true, "driver_license": true}

// submitDocuments carries the uploaded document fileIds referenced by a submission.
type submitDocuments struct {
	Front  string `json:"front"`
	Selfie string `json:"selfie"`
	Back   string `json:"back"`
}

// submitRequest is the POST /kyc body: the base KycSubmission fields plus the
// (backward-compatible) level and documents references.
type submitRequest struct {
	FirstName   string          `json:"firstName"`
	LastName    string          `json:"lastName"`
	Country     string          `json:"country"`
	IDType      string          `json:"idType"`
	IDNumber    string          `json:"idNumber"`
	DateOfBirth string          `json:"dateOfBirth"`
	Level       int             `json:"level"`
	Documents   submitDocuments `json:"documents"`
}

// handleSubmit validates and persists a KYC application (basic level 1 or advanced
// level 2). See the per-step comments for the enforced rules.
//
//	POST /kyc
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req submitRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}

	// Normalise and validate the base fields.
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	req.IDType = strings.ToLower(strings.TrimSpace(req.IDType))
	req.IDNumber = strings.TrimSpace(req.IDNumber)
	if req.FirstName == "" || req.LastName == "" || req.Country == "" || req.IDNumber == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "姓名/国家/证件号码必填")
		return
	}
	if !validIDTypes[req.IDType] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "证件类型不合法")
		return
	}

	// Level: default 1 (basic); only 1 and 2 are valid.
	level := req.Level
	if level == 0 {
		level = 1
	}
	if level != 1 && level != 2 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "认证等级不合法")
		return
	}

	// Age: must have reached MinAge by now.
	dob, err := time.Parse("2006-01-02", strings.TrimSpace(req.DateOfBirth))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "出生日期格式应为 YYYY-MM-DD")
		return
	}
	if ageYears(dob, time.Now()) < s.cfg.MinAge {
		common.WriteError(w, http.StatusBadRequest, "UNDERAGE", "未满足最低年龄要求")
		return
	}

	// Documents: front + selfie are required and must belong to this user with the
	// matching side; back is optional.
	frontID, ok := s.ownDoc(w, r, uid, req.Documents.Front, sideFront)
	if !ok {
		return
	}
	selfieID, ok := s.ownDoc(w, r, uid, req.Documents.Selfie, sideSelfie)
	if !ok {
		return
	}
	app := Application{
		UserID: uid, Level: level, FirstName: req.FirstName, LastName: req.LastName,
		Country: req.Country, IDType: req.IDType, IDNumber: req.IDNumber, DateOfBirth: dob,
	}
	app.FrontDocID.Int64, app.FrontDocID.Valid = frontID, true
	app.SelfieDocID.Int64, app.SelfieDocID.Valid = selfieID, true
	if strings.TrimSpace(req.Documents.Back) != "" {
		backID, ok := s.ownDoc(w, r, uid, req.Documents.Back, sideBack)
		if !ok {
			return
		}
		app.BackDocID.Int64, app.BackDocID.Valid = backID, true
	}

	// Advanced (level 2) requires an approved basic (level 1) first.
	if level == 2 {
		approved, err := s.store.HighestApprovedLevel(r.Context(), uid)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if approved < 1 {
			common.WriteError(w, http.StatusBadRequest, "KYC_LEVEL_REQUIRED", "请先完成基础认证")
			return
		}
	}

	// Only one pending application at a time (unique index also guards races).
	pending, err := s.store.HasPending(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if pending {
		common.WriteError(w, http.StatusConflict, "KYC_PENDING", "已有审核中的认证申请")
		return
	}

	// The identity document must not already be approved for another user.
	taken, err := s.store.IDNumberApprovedByOther(r.Context(), uid, req.Country, req.IDType, req.IDNumber)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if taken {
		common.WriteError(w, http.StatusConflict, "ID_NUMBER_TAKEN", "该证件已被其他账户认证")
		return
	}

	out, err := s.store.InsertApplication(r.Context(), app)
	if err != nil {
		switch err {
		case ErrPendingExists:
			common.WriteError(w, http.StatusConflict, "KYC_PENDING", "已有审核中的认证申请")
		case ErrIDNumberTaken:
			common.WriteError(w, http.StatusConflict, "ID_NUMBER_TAKEN", "该证件已被其他账户认证")
		default:
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		}
		return
	}
	common.WriteJSON(w, http.StatusAccepted, applicationStatusDTO(out))
}

// ownDoc resolves a referenced fileId, verifying it is a valid id owned by uid
// with the expected side. On failure it writes an API error and returns false.
func (s *Server) ownDoc(w http.ResponseWriter, r *http.Request, uid, fileID, wantSide string) (int64, bool) {
	id, ok := parseDocID(fileID)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少或非法的"+sideLabel(wantSide)+"文件")
		return 0, false
	}
	doc, err := s.store.OwnedDocument(r.Context(), uid, id)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "DOCUMENT_NOT_FOUND", sideLabel(wantSide)+"文件不存在或不属于当前用户")
		return 0, false
	}
	if doc.Side != wantSide {
		common.WriteError(w, http.StatusBadRequest, "DOCUMENT_SIDE_MISMATCH", sideLabel(wantSide)+"文件类型不匹配")
		return 0, false
	}
	return id, true
}

// handleStatus returns the user's latest application state (none if the user has
// never submitted). The entitlement level used by the wallet lives on the account
// (synced on approval); this reflects the latest application's level/status.
//
//	GET /kyc/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	app, err := s.store.LatestApplication(r.Context(), uid)
	if err == ErrNotFound {
		common.WriteJSON(w, http.StatusOK, statusDTO{Level: 0, Status: statusNone})
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, applicationStatusDTO(app))
}

// applicationStatusDTO maps an application onto the openapi KycStatus shape.
func applicationStatusDTO(a *Application) statusDTO {
	dto := statusDTO{
		Level:       a.Level,
		Status:      a.Status,
		SubmittedAt: strptr(a.SubmittedAt.UTC().Format(time.RFC3339)),
	}
	if a.Reason.Valid {
		dto.Reason = strptr(a.Reason.String)
	}
	return dto
}

// ageYears returns the number of whole years between dob and now.
func ageYears(dob, now time.Time) int {
	years := now.Year() - dob.Year()
	// Subtract one if this year's birthday has not occurred yet.
	anniversary := time.Date(now.Year(), dob.Month(), dob.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.Before(anniversary) {
		years--
	}
	return years
}

// sideLabel renders a human label for a document side (used in error messages).
func sideLabel(side string) string {
	switch side {
	case sideFront:
		return "证件正面"
	case sideSelfie:
		return "手持证件照"
	case sideBack:
		return "证件背面"
	default:
		return "证件"
	}
}
