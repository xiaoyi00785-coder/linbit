package main

import (
	"errors"
	"net/http"
)

// apiError is a typed business error surfaced to the client as a JSON envelope.
type apiError struct {
	Status        int
	Code, Message string
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func bad(code, msg string) error { return &apiError{http.StatusBadRequest, code, msg} }

var (
	errNotFound       = &apiError{http.StatusNotFound, "NOT_FOUND", "记录不存在"}
	errMarkUnavail    = &apiError{http.StatusServiceUnavailable, "MARK_PRICE_UNAVAILABLE", "标记价格暂不可用，请稍后再试"}
	errInsufficient   = bad("INSUFFICIENT_MARGIN", "可用保证金不足")
	errInstrInactive  = bad("INSTRUMENT_INACTIVE", "该期权合约不可交易")
)

func asAPI(err error) (*apiError, bool) {
	var a *apiError
	ok := errors.As(err, &a)
	return a, ok
}
