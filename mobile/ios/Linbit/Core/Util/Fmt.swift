import Foundation

/// 价格/数量一律用 Decimal（服务端为十进制字符串），禁止转 Double 参与计算。
enum Dec {
    static let posix = Locale(identifier: "en_US_POSIX")
    static func parse(_ s: String?) -> Decimal { s.flatMap { Decimal(string: $0, locale: posix) } ?? 0 }
    /// 小数位数（0.1 → 1，10 → 0）
    static func decimals(_ d: Decimal) -> Int {
        let s = NSDecimalNumber(decimal: d).stringValue
        guard let dot = s.firstIndex(of: ".") else { return 0 }
        return s.distance(from: dot, to: s.endIndex) - 1
    }
}

enum Fmt {
    /// 行情页每次重算要格式化上百个数字。NumberFormatter 创建很贵，按配置缓存复用。
    /// 创建后不再修改，string(from:) 可多线程调用；字典读写加锁。
    private static var cache: [Int: NumberFormatter] = [:]
    private static let lock = NSLock()

    private static func formatter(_ minF: Int, _ maxF: Int, grouping: Bool = true) -> NumberFormatter {
        let key = minF << 8 | maxF << 1 | (grouping ? 1 : 0)
        lock.lock(); defer { lock.unlock() }
        if let f = cache[key] { return f }
        let f = NumberFormatter()
        f.locale = Dec.posix
        f.numberStyle = .decimal
        f.usesGroupingSeparator = grouping
        f.groupingSeparator = ","
        f.minimumFractionDigits = minF
        f.maximumFractionDigits = maxF
        f.roundingMode = .halfUp
        cache[key] = f
        return f
    }

    /// 按交易对精度显示价格，例如 81,133.25。
    static func price(_ d: Decimal, precision: Int) -> String {
        formatter(precision, precision).string(from: d as NSDecimalNumber) ?? "--"
    }

    /// 未知精度时按量级自适应（与对照图一致：≥1 两位小数，<1 四位有效小数）。
    static func priceAuto(_ d: Decimal) -> String {
        let a = abs(d)
        let p = a >= 1000 ? 2 : a >= 1 ? 4 : a >= 0.01 ? 5 : 8
        var s = price(d, precision: p)
        if a >= 1, s.contains(".") { // 去掉多余尾零，但至少保留 2 位
            while s.hasSuffix("0"), let dot = s.firstIndex(of: "."), s.distance(from: dot, to: s.endIndex) > 3 { s.removeLast() }
        }
        return s
    }

    /// 涨跌幅：+0.66% / -1.13%（输入为百分数字符串值，如 "0.66"）。
    /// 数量/金额：固定小数位、向下截断、千分位
    static func amount(_ d: Decimal, _ prec: Int) -> String {
        var v = d, r = Decimal(); NSDecimalRound(&r, &v, prec, .down)
        return formatter(prec, prec).string(from: r as NSDecimalNumber) ?? "0"
    }
    /// 盘口数量：≥1000 用 K/M 缩写（对照图 #28）
    static func compact(_ d: Decimal) -> String {
        if d >= 1_000_000 { return amount(d / 1_000_000, 2) + "M" }
        if d >= 1_000 { return amount(d / 1_000, 2) + "K" }
        return amount(d, d >= 100 ? 4 : 5)
    }
    static func percent(_ d: Decimal) -> String {
        let s = formatter(2, 2, grouping: false).string(from: d as NSDecimalNumber) ?? "0.00"
        return d >= 0 ? "+\(s)%" : "\(s)%"
    }

    /// 成交额：万 / 亿（对照图「4640.14万」「12.94亿」）。
    static func volumeCN(_ d: Decimal) -> String {
        let f = formatter(2, 2, grouping: false)
        if d >= 100_000_000 { return (f.string(from: (d / 100_000_000) as NSDecimalNumber) ?? "") + "亿" }
        if d >= 10_000 { return (f.string(from: (d / 10_000) as NSDecimalNumber) ?? "") + "万" }
        return formatter(0, 3, grouping: false).string(from: d as NSDecimalNumber) ?? "0"
    }

    /// 法币折合：¥ 541,066.4
    static func fiat(_ usdt: Decimal, rate: Decimal) -> String {
        "¥" + price(usdt * rate, precision: 2)
    }
}

extension Fmt {
    private static let dateTimeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Dec.posix
        f.dateFormat = "yyyy-MM-dd HH:mm"
        return f
    }()
    static func dateTime(_ d: Date) -> String { dateTimeFormatter.string(from: d) }

    private static var dateCache: [String: DateFormatter] = [:]
    /// 固定格式的本地时间（en_US_POSIX），格式化器按格式缓存
    static func date(_ d: Date, _ format: String) -> String {
        lock.lock(); defer { lock.unlock() }
        let f = dateCache[format] ?? {
            let f = DateFormatter(); f.locale = Dec.posix; f.dateFormat = format
            dateCache[format] = f
            return f
        }()
        return f.string(from: d)
    }
}
