//! 价格和数量的定点小数。
//!
//! 金额都是带 [`SCALE`] 位小数的 `i128`（1.0 == [`ONE`]）。撮合热路径只做整数运算，
//! 价格和数量不经过浮点。名义金额和手续费由 Go 结算层用自己的任意精度小数重算，
//! 引擎只负责比价和扣减数量。内部统一 8 位小数，覆盖当前所有交易对的精度配置。

/// 内部保留的小数位数。
pub const SCALE: u32 = 8;

/// `1.0` 的定点表示。
pub const ONE: i128 = 100_000_000; // 10^SCALE

/// 把十进制字符串（例如 "76768.40"）解析成定点整数。
///
/// 非法格式，以及小数位超过 [`SCALE`] 的值，直接拒绝。精度校验在上游 Go 订单服务已经做过，
/// 这里多出来的小数位是调用错误，不能静默截断。
pub fn parse(s: &str) -> Result<i128, String> {
    let s = s.trim();
    if s.is_empty() {
        return Err("empty decimal".into());
    }
    let (neg, body) = match s.strip_prefix('-') {
        Some(rest) => (true, rest),
        None => (false, s.strip_prefix('+').unwrap_or(s)),
    };
    let (int_part, frac_part) = match body.split_once('.') {
        Some((i, f)) => (i, f),
        None => (body, ""),
    };
    if int_part.is_empty() && frac_part.is_empty() {
        return Err(format!("invalid decimal {s:?}"));
    }
    if frac_part.len() as u32 > SCALE {
        return Err(format!("too many fractional digits in {s:?}"));
    }
    if !int_part.chars().all(|c| c.is_ascii_digit())
        || !frac_part.chars().all(|c| c.is_ascii_digit())
    {
        return Err(format!("invalid decimal {s:?}"));
    }
    let int_val: i128 = if int_part.is_empty() {
        0
    } else {
        int_part.parse().map_err(|_| format!("overflow in {s:?}"))?
    };
    let mut frac_val: i128 = 0;
    for c in frac_part.chars() {
        frac_val = frac_val * 10 + (c as u8 - b'0') as i128;
    }
    // 小数部分补零到 SCALE 位后再并入整数。
    for _ in 0..(SCALE - frac_part.len() as u32) {
        frac_val *= 10;
    }
    let v = int_val * ONE + frac_val;
    Ok(if neg { -v } else { v })
}

/// 定点整数格式化回十进制字符串。小数部分去掉末尾的 0（`70000.00000000` 变成 `"70000"`）。
pub fn format(v: i128) -> String {
    let neg = v < 0;
    let v = v.abs();
    let int_part = v / ONE;
    let frac_part = v % ONE;
    let mut out = String::new();
    if neg {
        out.push('-');
    }
    if frac_part == 0 {
        out.push_str(&int_part.to_string());
        return out;
    }
    let mut frac = format!("{:08}", frac_part);
    while frac.ends_with('0') {
        frac.pop();
    }
    out.push_str(&int_part.to_string());
    out.push('.');
    out.push_str(&frac);
    out
}

/// 报价预算 `budget` 在价格 `price` 上能买到的数量（两者都是定点）：`budget/price`，向下取整。
/// 用于按报价金额成交的市价买单。
pub fn qty_for_budget(budget: i128, price: i128) -> i128 {
    if price <= 0 {
        return 0;
    }
    // 定点：数量 = 预算 * ONE / 价格。整数除法，余数丢掉，即向下取整。
    budget * ONE / price
}

/// 名义金额 = 价格 * 数量（均为定点），结果仍是定点报价金额，向下取整。
pub fn notional(price: i128, qty: i128) -> i128 {
    price * qty / ONE
}

/// 把定点数量向下取整到 `precision` 位小数。`precision >= SCALE` 时原样返回。
pub fn floor_to(v: i128, precision: u32) -> i128 {
    if precision >= SCALE {
        return v;
    }
    let mut step = 1i128;
    for _ in 0..(SCALE - precision) {
        step *= 10;
    }
    v - (v % step)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_and_format_roundtrip() {
        for s in ["0", "1", "70000", "76768.4", "0.01", "0.00000001", "1234.5678"] {
            assert_eq!(format(parse(s).unwrap()), s.trim_start_matches('+'));
        }
    }

    #[test]
    fn parse_pads_fraction() {
        assert_eq!(parse("1.5").unwrap(), 150_000_000);
        assert_eq!(parse("0.01").unwrap(), 1_000_000);
        assert_eq!(parse("70000").unwrap(), 7_000_000_000_000);
    }

    #[test]
    fn rejects_bad_input() {
        assert!(parse("").is_err());
        assert!(parse("abc").is_err());
        assert!(parse("1.234567890").is_err()); // 9 fractional digits
        assert!(parse(".").is_err());
    }

    #[test]
    fn notional_and_budget() {
        let price = parse("70000").unwrap();
        let qty = parse("0.01").unwrap();
        assert_eq!(format(notional(price, qty)), "700");
        // 700 quote at 70000 buys 0.01
        let budget = parse("700").unwrap();
        assert_eq!(format(qty_for_budget(budget, price)), "0.01");
    }

    #[test]
    fn floor_to_precision() {
        let v = parse("0.123456789").unwrap_or(12_345_678); // already <=8 dp
        let v = floor_to(v, 6);
        assert_eq!(format(v), "0.123456");
    }
}
