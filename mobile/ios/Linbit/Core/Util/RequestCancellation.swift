import Foundation

/// 请求被取消（切换 Tab / 页面消失导致 `.task(id:)` 取消）时不应当作错误展示。
/// URLSession 取消会抛 URLError.cancelled，经 APIClient 包成 APIFailure.transport。
func isRequestCancelled(_ error: Error) -> Bool {
    if error is CancellationError { return true }
    if let f = error as? APIFailure, case let .transport(inner) = f { return isRequestCancelled(inner) }
    return (error as? URLError)?.code == .cancelled
}
