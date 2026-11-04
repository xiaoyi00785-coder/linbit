import SwiftUI

/// 截图演示状态：SecShot.raw = "<prefix>:<state>"
enum DemoShot {
    static func state(_ prefix: String) -> String? {
        guard let r = SecShot.raw, r.hasPrefix(prefix + ":") else { return nil }
        return String(r.dropFirst(prefix.count + 1))
    }
    static var nowMs: Int64 { Int64(Date().timeIntervalSince1970 * 1000) }
    static func pct(_ s: String?, _ prec: Int = 2) -> String {
        guard let s, !s.isEmpty else { return "--" }
        return Fmt.amount(Dec.parse(s) * 100, prec) + "%"
    }
    static func ms(_ v: Int64?) -> String { v.map { Fmt.dateTime(Date(timeIntervalSince1970: Double($0) / 1000)) } ?? "--" }
}

/// 宽松 JSON：字段缺失 / 数字或字符串混用都不报错（后端 decimal 多为字符串，时间可能带纳秒）
enum J: Decodable, Equatable, ExpressibleByDictionaryLiteral, ExpressibleByArrayLiteral, ExpressibleByStringInterpolation,
    ExpressibleByIntegerLiteral, ExpressibleByFloatLiteral, ExpressibleByBooleanLiteral {
    init(dictionaryLiteral els: (String, J)...) { self = .obj(Dictionary(els, uniquingKeysWith: { $1 })) }
    init(arrayLiteral els: J...) { self = .arr(els) }
    init(stringLiteral v: String) { self = .str(v) }
    init(integerLiteral v: Int) { self = .num(Double(v)) }
    init(floatLiteral v: Double) { self = .num(v) }
    init(booleanLiteral v: Bool) { self = .bool(v) }

    case null, bool(Bool), num(Double), str(String), arr([J]), obj([String: J])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        else if let d = try? c.decode(Double.self) { self = .num(d) }
        else if let s = try? c.decode(String.self) { self = .str(s) }
        else if let a = try? c.decode([J].self) { self = .arr(a) }
        else if let o = try? c.decode([String: J].self) { self = .obj(o) }
        else { self = .null }
    }

    init(_ v: Int64) { self = .num(Double(v)) }
    init(_ v: Int) { self = .num(Double(v)) }
    init(_ v: Double) { self = .num(v) }
    init(_ v: [J]) { self = .arr(v) }

    subscript(_ k: String) -> J { if case let .obj(o) = self { return o[k] ?? .null }; return .null }
    var s: String? {
        switch self {
        case let .str(v): return v
        case let .num(v): return v == v.rounded() && abs(v) < 1e15 ? String(Int64(v)) : String(v)
        case let .bool(v): return v ? "true" : "false"
        default: return nil
        }
    }
    var d: Decimal { Dec.parse(s) }
    var i: Int64? { if case let .num(v) = self { return Int64(v) }; return s.flatMap { Int64($0) } }
    var b: Bool { if case let .bool(v) = self { return v }; return s == "true" }
    var a: [J] { if case let .arr(v) = self { return v }; return [] }
    var isNull: Bool { self == .null }
    /// items 数组（{items:[...]} 或直接数组）
    var items: [J] { if case .arr = self { return a }; return self["items"].a }
    /// 时间：毫秒 / 秒时间戳或 ISO8601 字符串
    var time: String {
        if case let .num(v) = self { return Fmt.dateTime(Date(timeIntervalSince1970: v > 1e12 ? v / 1000 : v)) }
        guard let t = s, !t.isEmpty else { return "--" }
        if t.count == 10 { return t }
        let f = ISO8601DateFormatter(); f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        var t2 = t
        if let dot = t.firstIndex(of: "."), let end = t[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
            let frac = t[t.index(after: dot)..<end].prefix(3)
            t2 = String(t[..<dot]) + "." + frac.padding(toLength: 3, withPad: "0", startingAt: 0) + String(t[end...])
        }
        if let dt = f.date(from: t2) { return Fmt.dateTime(dt) }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: t).map { Fmt.dateTime($0) } ?? t
    }
}

/// 金额显示
func jAmt(_ v: J, _ prec: Int = 2) -> String { v.isNull ? "--" : Fmt.amount(v.d, prec) }
/// 带符号金额
func jSigned(_ v: J, _ prec: Int = 2) -> String { v.isNull ? "--" : (v.d > 0 ? "+" : "") + Fmt.amount(v.d, prec) }
/// 比例（0.3 → 30.00%）
func jRatio(_ v: J, _ prec: Int = 2) -> String { DemoShot.pct(v.s, prec) }

struct KV: View {
    @Environment(\.palette) private var p
    let k: String; let v: String; var color: Color? = nil
    var body: some View {
        HStack {
            Text(k).font(.lb(FontSize.small)).foregroundColor(p.textSecondary)
            Spacer()
            Text(v).font(.lbNum(FontSize.small, .medium)).foregroundColor(color ?? p.textPrimary).lineLimit(1).minimumScaleFactor(0.7)
        }
    }
}

struct StatTile: View {
    @Environment(\.palette) private var p
    let title: String; let value: String; var color: Color? = nil
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.lb(FontSize.small)).foregroundColor(p.textSecondary).lineLimit(1)
            Text(value).font(.lbNum(17, .semibold)).foregroundColor(color ?? p.textPrimary).lineLimit(1).minimumScaleFactor(0.6)
        }
        .frame(maxWidth: .infinity, alignment: .leading).padding(12)
        .background(RoundedRectangle(cornerRadius: Radius.md).fill(p.fill))
    }
}

extension View {
    func lbCard() -> some View { modifier(LBCardMod()) }
}
struct LBCardMod: ViewModifier {
    @Environment(\.palette) private var p
    func body(content: Content) -> some View {
        content.padding(14).frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: Radius.lg).fill(p.bg))
            .overlay(RoundedRectangle(cornerRadius: Radius.lg).stroke(p.border, lineWidth: 1))
    }
}
