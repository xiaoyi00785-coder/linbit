#!/usr/bin/env bash
# 测试网 → 主网数据清理。仅人工执行；默认 dry-run。计划见 docs/deploy/mainnet-wipe-plan.md
# 用法: BACKUP_FILE=/backup/linbit.dump [CONFIRM_WIPE=WIPE-TESTNET-<db>] deploy/mainnet/wipe-testnet.sh [--execute]
# 需要 DATABASE_URL（从运维 .env 读取，脚本不打印它）。
set -euo pipefail
WIPE_TABLES=(announcements users account_settings devices login_challenges notification_prefs notifications passkeys password_reset_tokens refresh_tokens sub_account_settings sub_accounts sub_api_keys sub_transfers user_favorites verification_codes vip_applications webauthn_sessions withdraw_addresses admin_sessions reward_grants solvency_reports withdrawal_reviews convert_pair_state asset_accounts balances funding_balances freeze_accounts ledger_entries ledger_transactions settlements wallet_ops convert_events convert_hedges convert_orders convert_quotes e_earn_events e_fixed_orders e_holdings e_loan_collaterals e_loan_events e_loans e_unlocks ev_index_samples ev_orders fut_accounts fut_adl fut_algo_orders fut_bills fut_bonus fut_copy_events fut_copy_follows fut_copy_settlements fut_copy_traders fut_copy_trades fut_fee_overrides fut_funding_history fut_insurance_snapshots fut_liquidations fut_mark_history fut_orders fut_position_history fut_position_mode fut_positions fut_trades fut_transfers g_commissions g_coupons g_cursors g_enrollments g_fee_events g_newcomer g_newcomer_claims g_partners g_payouts g_points_daily g_redemptions g_settlements g_users g_wallet_ops kyc_applications kyc_documents kyc_level_sync chat_messages chat_reports chat_sanctions klines por_snapshots mm_inventory_baseline orders trades spot_conditional_orders spot_fee_overrides grid_bots grid_orders grid_snapshots chain_cursors chain_state chain_txs deposit_addresses deposits internal_transfers sweeps withdrawals)
: "${DATABASE_URL:?DATABASE_URL required}"
: "${BACKUP_FILE:?BACKUP_FILE required: take pg_dump -Fc first}"
[ -s "$BACKUP_FILE" ] || { echo "backup file missing or empty: $BACKUP_FILE" >&2; exit 1; }
pg_restore -l "$BACKUP_FILE" >/dev/null || { echo "backup is not a valid pg_dump -Fc archive" >&2; exit 1; }
DB=$(psql "$DATABASE_URL" -Atc 'select current_database()')
existing=()
for t in "${WIPE_TABLES[@]}"; do
  if [ "$(psql "$DATABASE_URL" -Atc "select to_regclass('$t') is not null")" = t ]; then existing+=("$t"); else echo "skip (not found): $t"; fi
done
echo "database: $DB"; echo "will TRUNCATE ${#existing[@]} tables:"; printf '  %s\n' "${existing[@]}"
echo "will UPDATE mm_state SET running=false"
echo "id sequences are NOT reset (no RESTART IDENTITY); uid_seq is bumped to >= max(users.uid) so new uids never reuse ids in audit_logs"
if [ "${1:-}" != "--execute" ]; then echo "dry-run only. Re-run with --execute and CONFIRM_WIPE=WIPE-TESTNET-$DB"; exit 0; fi
[ "${CONFIRM_WIPE:-}" = "WIPE-TESTNET-$DB" ] || { echo "refused: set CONFIRM_WIPE=WIPE-TESTNET-$DB" >&2; exit 1; }
read -r -p "Type the database name ($DB) to confirm: " ans; [ "$ans" = "$DB" ] || { echo "aborted"; exit 1; }
list=$(IFS=,; echo "${existing[*]}")
# No CASCADE: if a kept table references a wiped one, this fails safely.
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;
-- uid_seq is never reset: make sure it is at least the highest uid ever issued.
DO \$\$ BEGIN
  IF to_regclass('uid_seq') IS NOT NULL AND to_regclass('users') IS NOT NULL THEN
    PERFORM setval('uid_seq', GREATEST((SELECT last_value FROM uid_seq),
      COALESCE((SELECT max(uid::bigint) FROM users WHERE uid ~ '^[0-9]+\$'), 0)));
  END IF;
END \$\$;
-- No RESTART IDENTITY: ids keep growing, so nothing collides with kept audit_logs.
TRUNCATE $list;
DO \$\$ BEGIN IF to_regclass('mm_state') IS NOT NULL THEN UPDATE mm_state SET running=false; END IF; END \$\$;
COMMIT;
SQL
psql "$DATABASE_URL" -Atc "select 'uid_seq next > ' || last_value from uid_seq" 2>/dev/null || true
echo "done. Now reset the enginedata volume, FLUSHALL redis, clear kycdocs files (see plan)."
