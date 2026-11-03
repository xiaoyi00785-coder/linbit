//! Fixed-point decimal arithmetic for prices and quantities.
//!
//! All amounts are represented as scaled `i128` integers with [`SCALE`] decimal
//! places (1.0 == [`ONE`]). This keeps the matching hot path on exact integer
//! math — no floating point ever touches an order's price or quantity. The Go
//! settlement layer re-derives notionals and fees with its own arbitrary-
//! precision decimal, so the engine only needs to compare prices and subtract
//! quantities; eight decimal places comfortably covers every configured symbol.

/// Number of decimal places kept internally.
pub const SCALE: u32 = 8;

/// The scaled representation of `1.0`.
pub const ONE: i128 = 100_000_000; // 10^SCALE

/// Parse a decimal string (e.g. "76768.40") into a scaled integer.
///
/// Rejects malformed input and any value with more than [`SCALE`] fractional
/// digits — callers upstream (the Go order service) already validate precision,
/// so extra digits indicate a bug rather than something to silently truncate.
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
    // Pad the fractional part up to SCALE digits.
    for _ in 0..(SCALE - frac_part.len() as u32) {
        frac_val *= 10;
    }
    let v = int_val * ONE + frac_val;
    Ok(if neg { -v } else { v })
}

/// Format a scaled integer back to a canonical decimal string. Trailing zeros
/// in the fractional part are trimmed (e.g. `70000.00000000` -> `"70000"`).
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

/// Quantity buyable with `budget` quote at `price` (both scaled): `budget/price`
/// as a scaled quantity, rounded down. Used for market-buy-by-quote matching.
pub fn qty_for_budget(budget: i128, price: i128) -> i128 {
    if price <= 0 {
        return 0;
    }
    // qty_scaled = budget_scaled * ONE / price_scaled
    budget * ONE / price
}

/// Notional = price * qty (both scaled) as a scaled quote amount, rounded down.
pub fn notional(price: i128, qty: i128) -> i128 {
    price * qty / ONE
}

/// Round a scaled quantity down to `precision` decimal places.
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
