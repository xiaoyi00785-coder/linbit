package main

import (
	"errors"
	"github.com/linbit/platform/services/go/common"
	"net/http"
)

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string                   { return e.Message }
func apiErr(status int, code, message string) error { return &apiError{status, code, message} }
func writeErr(w http.ResponseWriter, err error) {
	var e *apiError
	if errors.As(err, &e) {
		common.WriteError(w, e.Status, e.Code, e.Message)
		return
	}
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}
