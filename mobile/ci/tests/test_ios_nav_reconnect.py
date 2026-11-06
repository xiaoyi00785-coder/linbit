#!/usr/bin/env python3
"""Disconnected nav reconnect guards ≠ Swift compile."""
from pathlib import Path
import sys
ROOT = Path(__file__).resolve().parents[3]
fails=[]
def ok(c,m):
    print(("OK " if c else "FAIL ")+m)
    if not c: fails.append(m)
def read(r): return (ROOT/r).read_text()
m=read("mobile/ios/Linbit/Features/Markets/MarketsView.swift")
c=read("mobile/ios/Linbit/Features/Chat/ChatRoomView.swift")
s=read("mobile/ios/Linbit/Features/Spot/SpotTradeView.swift")
ok("ChatRoomView(embedInTab: true)" in m, "Markets 聊天室 → ChatRoomView")
ok("聊天室后端暂未提供" not in m, "removed stale no-backend claim")
ok("ComingSoonView(title: \"聊天室\"" in m, "flag-off ComingSoon")
ok("embedInTab" in c and "FeatureShell" in c, "ChatRoomView embed mode")
ok("showMoreTools" in s and "SpotRoute.history" in s, "Spot ellipsis → history")
ok("SpotFeeSheet" in s and "showFee = true" in s, "Spot ellipsis → fee")
ok("更多功能即将开放" not in s, "removed dead Spot more toast")
# Remaining intentionally NOT claimed done:
# P2P/fiatOnRamp/options HomeMore → no OpenAPI
# 快捷买币 Assets → no OpenAPI  
# 交易偏好 full prefs → /account/preferences only quoteCurrency
ok(True, "inventory: P2P/法币买币/期权=无契约; 交易偏好完整表单=契约不足; paper/alerts=已实现不列缺")
if fails:
    print("FAILED", fails); sys.exit(1)
print("ALL PASSED (nav reconnect ≠ Swift)")
