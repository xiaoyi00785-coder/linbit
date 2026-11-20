package main

// compat_v3.go: path aliases for EasiCoin-style legacy URLs so the mobile shell
// (which still issues /user/private/v3/* calls) hits the current /account/*
// handlers without a client-side change.
//
// Aliases are rewritten before auth and routing, so the usual access control,
// client-IP handling and upstream selection all apply as if the client had
// called the canonical path directly. Keep the table narrow: an alias here
// hides the real route from gateway tests and from `grep -r "/account/"`.

// legacyAlias returns the canonical gateway path for a legacy EasiCoin URL.
// The second return is false for an unknown path.
//
// Only paths the iOS/Flutter shell still uses are listed. Adding more over
// time is fine, but the direction is one-way: the real client should move to
// the canonical paths rather than keep this list growing.
func legacyAlias(p string) (string, bool) {
	switch p {
	// Account deletion (POST create / POST cancel / GET status).
	case "/user/private/v3/account-delete":
		return "/account/delete-request", true
	case "/user/private/v3/account-delete-cancel":
		return "/account/delete-cancel", true
	case "/user/private/v3/account-delete-status":
		return "/account/delete-status", true
	// Telegram binding (POST bind / POST unbind / GET status).
	case "/user/private/v3/bind-tg":
		return "/account/bind-telegram", true
	case "/user/private/v3/unbind-tg":
		return "/account/unbind-telegram", true
	case "/user/private/v3/tg-info":
		return "/account/telegram", true
	}
	return "", false
}
