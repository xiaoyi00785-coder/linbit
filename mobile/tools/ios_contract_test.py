#!/usr/bin/env python3
"""Linux 可跑的 iOS 契约回归：镜像 J.arr / 借币 flexible 判断，对照真实后端 JSON shape。

不替代 Xcode/iOS 模拟器构建（交 CI）。Swift 工具链本机不可用时仅跑本脚本。
"""
from __future__ import annotations

import json
import sys
from typing import Any


def j_arr(obj: Any, *keys: str) -> list:
    """镜像 DemoShot.J.arr / items：自身为数组，或按候选键取数组。"""
    if isinstance(obj, list):
        return obj
    if not isinstance(obj, dict):
        return []
    for k in keys:
        v = obj.get(k)
        if isinstance(v, list):
            return v
    return []


def loan_is_flexible(term: str | None, due_at_null: bool) -> bool:
    """镜像 LoansView.isFlexible：生产 term 为 flexible；dueAt 缺失亦视为活期。"""
    return term == "flexible" or due_at_null


def loan_term_name(term: str | None) -> str:
    """镜像 LoansView.termName。"""
    if term is None:
        return "--"
    if term in ("flexible", "flex"):  # flex 仅兼容旧值，生产用 flexible
        return "灵活"
    if term.endswith("d") and term[:-1].isdigit():
        return f"{term[:-1]} 天"
    return term


def loan_show_renew(term: str | None, due_at_null: bool) -> bool:
    return not loan_is_flexible(term, due_at_null)


def follow_body(view: dict) -> dict:
    nested = view.get("follow")
    if isinstance(nested, dict):
        return nested
    return view


def test_copy_traders_shape() -> None:
    # 后端 hCopyTraders / openapi CopyTraderList
    prod = {"traders": [{"traderId": "T1", "nickname": "真实交易员", "roi": "0.25"}], "days": 30, "sort": "roi"}
    demo_wrong = {"items": [{"traderId": "T1"}]}  # 旧 Demo
    assert j_arr(prod, "traders", "items")[0]["nickname"] == "真实交易员"
    assert j_arr(prod, "items") == []  # 只认 items 会空 —— 回归审查阻塞
    assert j_arr(demo_wrong, "traders", "items")[0]["traderId"] == "T1"  # 兼容旧键
    assert j_arr(prod["traders"], "traders", "items")[0]["traderId"] == "T1"  # 直接数组


def test_copy_follows_shape() -> None:
    # 后端 hMyFollows：follows + followView 嵌套
    prod = {
        "follows": [
            {
                "follow": {"id": 9, "traderId": "T1", "status": "active", "mode": "ratio", "principal": "100"},
                "traderNickname": "真实交易员",
                "pnl": "3.25",
            }
        ]
    }
    rows = j_arr(prod, "follows", "items")
    assert len(rows) == 1
    assert j_arr(prod, "items") == []  # 只认 items 会空
    body = follow_body(rows[0])
    assert body["id"] == 9
    assert rows[0]["traderNickname"] == "真实交易员"
    assert rows[0]["pnl"] == "3.25"
    # 空列表
    assert j_arr({"follows": []}, "follows", "items") == []


def test_loan_flexible_renew() -> None:
    assert loan_is_flexible("flexible", False) is True
    assert loan_show_renew("flexible", False) is False  # 活期隐藏续期
    assert loan_is_flexible("flex", False) is False  # 旧错误值不再当活期
    assert loan_show_renew("7d", False) is True
    assert loan_show_renew("30d", False) is True
    assert loan_show_renew(None, True) is False  # dueAt 空 → 活期
    assert loan_term_name("flexible") == "灵活"
    assert loan_term_name("7d") == "7 天"
    assert loan_term_name("30d") == "30 天"


def main() -> int:
    tests = [test_copy_traders_shape, test_copy_follows_shape, test_loan_flexible_renew]
    failed = 0
    for t in tests:
        try:
            t()
            print(f"PASS {t.__name__}")
        except Exception as e:
            failed += 1
            print(f"FAIL {t.__name__}: {e}", file=sys.stderr)
    # 附带打印 fixture 样本，便于对照 openapi
    sample = {
        "traders": json.dumps({"traders": [{"traderId": "T1"}]}, ensure_ascii=False),
        "follows": json.dumps(
            {"follows": [{"follow": {"id": 1, "status": "active"}, "traderNickname": "N", "pnl": "1"}]},
            ensure_ascii=False,
        ),
    }
    print("fixtures:", sample)
    if failed:
        print(f"{failed} failed", file=sys.stderr)
        return 1
    print(f"OK {len(tests)} tests")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
