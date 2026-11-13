package main

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"
)

// lockFutUser is the cross-process lock for one user's futures wallet and
// positions. s.mu only serializes a single process; two replicas can otherwise
// read the same position, apply two fills, and write the qty back, dropping
// one of them. The advisory lock is held until the surrounding transaction
// commits, and it is re-entrant within that transaction.
//
// Callers must take it before any row lock (account or position) on that user.
// When a transaction touches several users, lock them in futLockKeys order.
func lockFutUser(ctx context.Context, q queryer, uid string) error {
	if uid == "" {
		return nil
	}
	_, err := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('fut:u:'||$1))`, uid)
	return err
}

// lockFutUsers locks each distinct user in sorted order so two transactions
// cannot deadlock by taking the same users in opposite orders. A deadlock can
// still happen if the caller already holds a later user; txLocked retries those.
func lockFutUsers(ctx context.Context, q queryer, uids ...string) error {
	for _, uid := range futLockKeys(uids...) {
		if err := lockFutUser(ctx, q, uid); err != nil {
			return err
		}
	}
	return nil
}

// futLockKeys returns the distinct, non-empty user ids in the order
// lockFutUsers acquires them.
func futLockKeys(uids ...string) []string {
	seen := make(map[string]struct{}, len(uids))
	out := make([]string, 0, len(uids))
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

func isDeadlock(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "40P01"
}
