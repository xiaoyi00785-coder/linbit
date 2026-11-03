import Foundation

/// 替代 Flight-School/AnyCodable 依赖的最小实现（保持工程零第三方依赖）。
public struct AnyCodable: Codable, Hashable, CustomStringConvertible {
    public let value: Any
    public init<T>(_ value: T?) { self.value = value ?? () }
    public var description: String { String(describing: value) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { value = () }
        else if let v = try? c.decode(Bool.self) { value = v }
        else if let v = try? c.decode(Int.self) { value = v }
        else if let v = try? c.decode(Double.self) { value = v }
        else if let v = try? c.decode(String.self) { value = v }
        else if let v = try? c.decode([AnyCodable].self) { value = v.map(\.value) }
        else if let v = try? c.decode([String: AnyCodable].self) { value = v.mapValues(\.value) }
        else { throw DecodingError.dataCorruptedError(in: c, debugDescription: "AnyCodable 无法解码") }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch value {
        case is Void: try c.encodeNil()
        case let v as Bool: try c.encode(v)
        case let v as Int: try c.encode(v)
        case let v as Double: try c.encode(v)
        case let v as String: try c.encode(v)
        case let v as [Any]: try c.encode(v.map { AnyCodable($0) })
        case let v as [String: Any]: try c.encode(v.mapValues { AnyCodable($0) })
        case let v as AnyCodable: try v.encode(to: encoder)
        default: try c.encodeNil()
        }
    }

    public static func == (l: AnyCodable, r: AnyCodable) -> Bool { l.description == r.description }
    public func hash(into h: inout Hasher) { h.combine(description) }
}
