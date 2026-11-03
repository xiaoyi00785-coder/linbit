import Foundation

/// 生成代码 _Models.swift 引用的类型；本工程不用生成的 URLSession 实现，网络请求走 Core/Network/APIClient。
public protocol URLSessionDataTaskProtocol {
    func cancel()
}
