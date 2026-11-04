#!/usr/bin/env bash
# 测试网 → 主网数据清理。仅人工执行；默认 dry-run。计划见 docs/deploy/mainnet-wipe-plan.md
# 用法: BACKUP_FILE=/backup/linbit.dump [KEEP_UIDS=80000004] [CONFIRM_WIPE=WIPE-TESTNET-<db>] deploy/mainnet/wipe-testnet.sh [--execute]
# 需要 DATABASE_URL（从运维 .env 读取，脚本不打印它）。
#
# KEEP_UIDS（逗号分隔的数字 uid，默认 80000004 = 验收账号，Yaeko 2026-11-03 决定保留；
# KEEP_UIDS=none 表示一个都不保留）：这些用户保留
#   账号:   users, notification_prefs, passkeys, user_favorites
#   资产:   asset_accounts, balances, funding_balances, fut_accounts, fut_position_mode
#   流水:   ledger_transactions(+ledger_entries)：user_id 为该用户，或任一分录记到 user:<uid>:*；fut_bills
#   KYC:    kyc_applications, kyc_documents, kyc_level_sync（kycdocs 卷里这些 file_key 的文件也必须保留）
# 其余（订单/成交/持仓/计划委托/理财/网格/充提记录/充值地址/会话/子账户/邀请关系/增长数据）全部清空。
# 因为挂单、冻结随引擎卷一起清掉：保留用户 locked → free（写 testnet_wipe_unlock 分录），fut_accounts.frozen=0。
# 若保留用户有持仓/理财/借贷/运行中网格，--execute 拒绝（先平仓/赎回），除非 ALLOW_DROP_KEPT_POSITIONS=1。
# 保留余额没有储备金：每个币种写一条 wallet_ops op=testnet_kept_unbacked，计入对账 virtualSeed（不计入应备付）。
set -euo pipefail
WIPE_TABLES=(announcements users account_settings devices login_challenges notification_prefs notifications passkeys password_reset_tokens refresh_tokens sub_account_settings sub_accounts sub_api_keys sub_transfers user_favorites verification_codes vip_applications webauthn_sessions withdraw_addresses admin_sessions reward_grants solvency_reports withdrawal_reviews convert_pair_state asset_accounts balances funding_balances freeze_accounts ledger_entries ledger_transactions settlements wallet_ops convert_events convert_hedges convert_orders convert_quotes e_earn_events e_fixed_orders e_holdings e_loan_collaterals e_loan_events e_loans e_unlocks ev_index_samples ev_orders fut_accounts fut_adl fut_algo_orders fut_bills fut_bonus fut_copy_events fut_copy_follows fut_copy_settlements fut_copy_traders fut_copy_trades fut_fee_overrides fut_funding_history fut_insurance_snapshots fut_liquidations fut_mark_history fut_orders fut_position_history fut_position_mode fut_positions fut_trades fut_transfers g_commissions g_coupons g_cursors g_enrollments g_fee_events g_newcomer g_newcomer_claims g_partners g_payouts g_points_daily g_redemptions g_settlements g_users g_wallet_ops kyc_applications kyc_documents kyc_level_sync chat_messages chat_reports chat_sanctions klines por_snapshots mm_inventory_baseline orders trades spot_conditional_orders spot_fee_overrides grid_bots grid_orders grid_snapshots chain_cursors chain_state chain_txs deposit_addresses deposits internal_transfers sweeps withdrawals)
# table:column — rows kept when column (text uid) is in KEEP_UIDS
KEEP_BY_UID=(asset_accounts:user_id balances:user_id funding_balances:user_id fut_accounts:user_id fut_position_mode:user_id fut_bills:user_id kyc_applications:user_id kyc_documents:user_id kyc_level_sync:user_id)
# table:column — rows kept when column (users.id bigint) belongs to a kept uid
KEEP_BY_ID=(notification_prefs:user_id passkeys:user_id user_favorites:user_id)
: "${DATABASE_URL:?DATABASE_URL required}"
: "${BACKUP_FILE:?BACKUP_FILE required: take pg_dump -Fc first}"
[ -s "$BACKUP_FILE" ] || { echo "backup file missing or empty: $BACKUP_FILE" >&2; exit 1; }
pg_restore -l "$BACKUP_FILE" >/dev/null || { echo "backup is not a valid pg_dump -Fc archive" >&2; exit 1; }
KEEP_UIDS="${KEEP_UIDS-80000004}"
[ "$KEEP_UIDS" = none ] && KEEP_UIDS=""
[[ "$KEEP_UIDS" =~ ^([0-9]+(,[0-9]+)*)?$ ]] || { echo "KEEP_UIDS must be comma-separated numeric uids or 'none'" >&2; exit 1; }
# Safe SQL literal (digits only, validated above).
if [ -n "$KEEP_UIDS" ]; then KA="ARRAY['${KEEP_UIDS//,/\',\'}']::text[]"; else KA="ARRAY[]::text[]"; fi
q() { psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -Atc "$1"; }
has() { [ "$(q "select to_regclass('$1') is not null")" = t ]; }
DB=$(q 'select current_database()')
declare -A KEEPCOND=()
KEEPCOND[users]="uid = ANY($KA)"
KEEPCOND[ledger_transactions]="user_id = ANY($KA) OR id IN (SELECT tx_id FROM ledger_entries WHERE split_part(account,':',1)='user' AND split_part(account,':',2) = ANY($KA))"
KEEPCOND[ledger_entries]="tx_id IN (SELECT id FROM ledger_transactions WHERE ${KEEPCOND[ledger_transactions]})"
for kv in "${KEEP_BY_UID[@]}"; do KEEPCOND[${kv%%:*}]="${kv#*:} = ANY($KA)"; done
for kv in "${KEEP_BY_ID[@]}"; do KEEPCOND[${kv%%:*}]="${kv#*:} IN (SELECT id FROM users WHERE uid = ANY($KA))"; done
truncs=(); filtered=()
for t in "${WIPE_TABLES[@]}"; do
  if ! has "$t"; then echo "skip (not found): $t"; continue; fi
  if [ -n "$KEEP_UIDS" ] && [ -n "${KEEPCOND[$t]:-}" ]; then filtered+=("$t"); else truncs+=("$t"); fi
done
echo "database: $DB"
if [ -n "$KEEP_UIDS" ]; then
  echo "KEEP_UIDS: $KEEP_UIDS"
  q "select '  kept user: uid='||uid||' id='||id||' email='||coalesce(email,'')||' kyc_level='||kyc_level from users where uid = ANY($KA) order by uid"
  for u in ${KEEP_UIDS//,/ }; do [ "$(q "select count(*) from users where uid='$u'")" = 1 ] || { echo "KEEP uid $u not found in users" >&2; exit 1; }; done
else echo "KEEP_UIDS: none (all users wiped)"; fi
echo "will TRUNCATE ${#truncs[@]} tables:"; printf '  %s\n' "${truncs[@]}"
echo "will DELETE all rows except KEEP_UIDS in ${#filtered[@]} tables (kept / total):"
for t in "${filtered[@]}"; do echo "  $t: $(q "select count(*) filter (where ${KEEPCOND[$t]})||' / '||count(*) from $t")"; done
blockers=0
if [ -n "$KEEP_UIDS" ]; then
  echo "kept balances (spot+funding free+locked; futures USDT wallet) — UNBACKED, recorded as testnet_kept_unbacked:"
  KEPT_SQL="SELECT user_id, asset, sum(amt) AS amt FROM (
      SELECT user_id, asset, free+locked AS amt FROM balances WHERE user_id = ANY($KA)
      UNION ALL SELECT user_id, asset, free+locked FROM funding_balances WHERE user_id = ANY($KA)
      $(has fut_accounts && echo "UNION ALL SELECT user_id, 'USDT', balance FROM fut_accounts WHERE user_id = ANY($KA)")
    ) x GROUP BY user_id, asset HAVING sum(amt) > 0"
  q "select '  uid='||user_id||' '||asset||' '||amt::text from ($KEPT_SQL) k order by 1"
  echo "  locked to release (spot/funding): $(q "select coalesce(sum(locked),0) from (select locked from balances where user_id = ANY($KA) union all select locked from funding_balances where user_id = ANY($KA)) l")"
  echo "kept user's items that will be DROPPED (value lost; must be 0 for --execute unless ALLOW_DROP_KEPT_POSITIONS=1):"
  chk() { if has "$1"; then n=$(q "select count(*) from $1 where $2"); echo "  $1: $n"; blockers=$((blockers+n)); fi; }
  chk fut_positions "user_id = ANY($KA) AND (qty <> 0 OR iso_margin <> 0)"
  chk e_holdings "uid = ANY($KA) AND (principal > 0 OR pending > 0)"
  chk e_fixed_orders "uid = ANY($KA) AND status = 'holding'"
  chk e_loans "uid = ANY($KA) AND status = 'active'"
  chk grid_bots "user_id = ANY($KA) AND status IN ('running','stopping')"
  echo "kept user's open orders / pending withdrawals dropped (their locked funds are released): orders=$(q "select count(*) from orders where user_id = ANY($KA) and status in ('new','partially_filled','open')" 2>/dev/null || echo ?) withdrawals=$(q "select count(*) from withdrawals where user_id = ANY($KA) and status not in ('completed','failed','rejected')" 2>/dev/null || echo ?)"
  if has kyc_documents; then echo "kycdocs files to KEEP (do not delete these file_keys):"; q "select '  '||file_key from kyc_documents where user_id = ANY($KA) order by id"; fi
fi
echo "will UPDATE mm_state SET running=false"
echo "id sequences are NOT reset (no RESTART IDENTITY); uid_seq is bumped to >= max(users.uid) so new uids never reuse ids in audit_logs"
if [ "${1:-}" != "--execute" ]; then echo "dry-run only. Re-run with --execute and CONFIRM_WIPE=WIPE-TESTNET-$DB"; exit 0; fi
[ "${CONFIRM_WIPE:-}" = "WIPE-TESTNET-$DB" ] || { echo "refused: set CONFIRM_WIPE=WIPE-TESTNET-$DB" >&2; exit 1; }
if [ "$blockers" -gt 0 ] && [ "${ALLOW_DROP_KEPT_POSITIONS:-}" != 1 ]; then
  echo "refused: kept users have $blockers open positions/earn/loans/grids; close them first or set ALLOW_DROP_KEPT_POSITIONS=1" >&2; exit 1; fi
read -r -p "Type the database name ($DB) to confirm: " ans; [ "$ans" = "$DB" ] || { echo "aborted"; exit 1; }
list=$(IFS=,; echo "${truncs[*]}")
# Delete order: children before parents (no CASCADE anywhere).
ORDER=(ledger_entries ledger_transactions kyc_applications kyc_documents kyc_level_sync notification_prefs passkeys user_favorites asset_accounts balances funding_balances fut_accounts fut_position_mode fut_bills users)
dels=""
if [ -n "$KEEP_UIDS" ]; then
  dels+="UPDATE users SET parent_user_id = NULL WHERE uid = ANY($KA);"$'\n'
  for t in "${ORDER[@]}"; do [[ " ${filtered[*]} " == *" $t "* ]] && dels+="DELETE FROM $t WHERE NOT (${KEEPCOND[$t]});"$'\n'; done
  post=""
  for bt in balances:spot funding_balances:funding; do
    t=${bt%%:*}; a=${bt#*:}; has "$t" || continue
    if [ "$a" = funding ]; then fb="'user:'||r.user_id||':funding_free'"; lb="'user:'||r.user_id||':funding_locked'"; else fb="'user:'||r.user_id||':free'"; lb="'user:'||r.user_id||':locked'"; fi
    post+="WITH r AS (SELECT user_id, asset, locked FROM $t WHERE user_id = ANY($KA) AND locked > 0),
      tx AS (INSERT INTO ledger_transactions (user_id, tx_type, ref) SELECT user_id, 'testnet_wipe_unlock', 'testnet_wipe_unlock:$a:'||user_id||':'||asset FROM r RETURNING id, user_id, ref)
      INSERT INTO ledger_entries (tx_id, account, asset, direction, amount)
      SELECT tx.id, e.account, r.asset, e.dir, r.locked FROM r JOIN tx ON tx.ref = 'testnet_wipe_unlock:$a:'||r.user_id||':'||r.asset
      CROSS JOIN LATERAL (VALUES ($fb, 'debit'), ($lb, 'credit')) e(account, dir);
    UPDATE $t SET free = free + locked, locked = 0 WHERE user_id = ANY($KA) AND locked > 0;"$'\n'
  done
  has fut_accounts && post+="UPDATE fut_accounts SET frozen = 0 WHERE user_id = ANY($KA);"$'\n'
  post+="INSERT INTO wallet_ops (ref, op, user_id, asset, amount) SELECT 'testnet_kept_unbacked:'||user_id||':'||asset, 'testnet_kept_unbacked', user_id, asset, amt FROM ($KEPT_SQL) k;"$'\n'
fi
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
$dels
${post:-}
DO \$\$ BEGIN IF to_regclass('mm_state') IS NOT NULL THEN UPDATE mm_state SET running=false; END IF; END \$\$;
COMMIT;
SQL
psql "$DATABASE_URL" -Atc "select 'uid_seq next > ' || last_value from uid_seq" 2>/dev/null || true
[ -n "$KEEP_UIDS" ] && { echo "kept rows after wipe:"; for t in "${filtered[@]}"; do echo "  $t: $(q "select count(*) from $t")"; done; }
echo "done. Now reset the enginedata volume, FLUSHALL redis, clear kycdocs files EXCEPT the kept file_keys listed above (see plan)."
