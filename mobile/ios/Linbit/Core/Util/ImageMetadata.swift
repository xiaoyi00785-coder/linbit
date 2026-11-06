import Foundation
import ImageIO

/// 上传前去掉图片里的隐私元数据（EXIF / GPS 定位 / 设备型号 / 拍摄时间 / IPTC / XMP 等）。
/// 保留方向信息（Orientation），避免图片被转正前后显示方向不一致。
/// 返回 nil 表示无法处理：调用方不要上传原图。
enum ImageMetadataStripper {
    static func strip(_ data: Data, mime: String) -> Data? {
        if mime == "image/webp" { return stripWebP(data) }
        return stripWithImageIO(data)
    }

    // MARK: JPEG / PNG：ImageIO 重写
    private static func stripWithImageIO(_ data: Data) -> Data? {
        guard let src = CGImageSourceCreateWithData(data as CFData, nil),
              let type = CGImageSourceGetType(src), CGImageSourceGetCount(src) > 0 else { return nil }
        let props = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as? [CFString: Any]
        let orientation = (props?[kCGImagePropertyOrientation] as? Int) ?? 1

        // 1) 直接复制图像并把各类元数据字典置空（不重新压缩像素）
        let null = NSNull()
        let opts: [CFString: Any] = [
            kCGImageMetadataShouldExcludeGPS: true,
            kCGImageMetadataShouldExcludeXMP: true,
            kCGImagePropertyExifDictionary: null,
            kCGImagePropertyExifAuxDictionary: null,
            kCGImagePropertyGPSDictionary: null,
            kCGImagePropertyTIFFDictionary: null,
            kCGImagePropertyIPTCDictionary: null,
            kCGImagePropertyMakerAppleDictionary: null,
            kCGImagePropertyOrientation: orientation,
        ]
        let out = NSMutableData()
        if let dest = CGImageDestinationCreateWithData(out, type, 1, nil) {
            CGImageDestinationAddImageFromSource(dest, src, 0, opts as CFDictionary)
            if CGImageDestinationFinalize(dest), !hasPrivateMetadata(out as Data) { return out as Data }
        }

        // 2) 兜底：只取像素重新编码，只带方向信息（JPEG 质量 0.9）
        guard let cg = CGImageSourceCreateImageAtIndex(src, 0, nil) else { return nil }
        let out2 = NSMutableData()
        guard let dest2 = CGImageDestinationCreateWithData(out2, type, 1, nil) else { return nil }
        let clean: [CFString: Any] = [kCGImagePropertyOrientation: orientation, kCGImageDestinationLossyCompressionQuality: 0.9]
        CGImageDestinationAddImage(dest2, cg, clean as CFDictionary)
        guard CGImageDestinationFinalize(dest2), !hasPrivateMetadata(out2 as Data) else { return nil }
        return out2 as Data
    }

    /// 输出里是否还残留定位 / 设备 / 时间等信息
    private static func hasPrivateMetadata(_ data: Data) -> Bool {
        guard let src = CGImageSourceCreateWithData(data as CFData, nil),
              let props = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as? [CFString: Any] else { return true }
        // 定位 / IPTC / 厂商私有字典只要存在就算残留
        if props[kCGImagePropertyGPSDictionary] != nil || props[kCGImagePropertyIPTCDictionary] != nil
            || props[kCGImagePropertyMakerAppleDictionary] != nil { return true }
        // 写出时 ImageIO 可能自带 Exif（像素宽高、色彩空间），只检查会泄露隐私的字段
        if let exif = props[kCGImagePropertyExifDictionary] as? [CFString: Any] {
            let keys: [CFString] = [kCGImagePropertyExifDateTimeOriginal, kCGImagePropertyExifDateTimeDigitized,
                                    kCGImagePropertyExifLensModel, kCGImagePropertyExifBodySerialNumber, kCGImagePropertyExifUserComment]
            if keys.contains(where: { exif[$0] != nil }) { return true }
        }
        if let tiff = props[kCGImagePropertyTIFFDictionary] as? [CFString: Any] {
            let keys: [CFString] = [kCGImagePropertyTIFFMake, kCGImagePropertyTIFFModel, kCGImagePropertyTIFFDateTime,
                                    kCGImagePropertyTIFFSoftware, kCGImagePropertyTIFFArtist]
            if keys.contains(where: { tiff[$0] != nil }) { return true }
        }
        return false
    }

    // MARK: WebP：删除 EXIF / XMP 块（无损），并清掉 VP8X 里对应的标志位
    private static func stripWebP(_ data: Data) -> Data? {
        let b = [UInt8](data)
        guard b.count >= 12, Array(b[0..<4]) == Array("RIFF".utf8), Array(b[8..<12]) == Array("WEBP".utf8) else { return nil }
        var out = Array(b[0..<12])
        var pos = 12
        while pos + 8 <= b.count {
            let tag = Array(b[pos..<pos + 4])
            var size = 0
            for i in 0..<4 { size |= Int(b[pos + 4 + i]) << (8 * i) } // 小端 32 位长度
            let end = pos + 8 + size + (size & 1) // 块按偶数字节对齐
            guard end <= b.count else { return nil }
            if tag == Array("EXIF".utf8) || tag == Array("XMP ".utf8) { pos = end; continue }
            var chunk = Array(b[pos..<end])
            if tag == Array("VP8X".utf8), size >= 10 { chunk[8] &= ~UInt8(0x08 | 0x04) } // 0x08 = EXIF，0x04 = XMP
            out += chunk
            pos = end
        }
        guard pos == b.count else { return nil }
        let riffSize = out.count - 8
        out[4] = UInt8(riffSize & 0xFF); out[5] = UInt8((riffSize >> 8) & 0xFF)
        out[6] = UInt8((riffSize >> 16) & 0xFF); out[7] = UInt8((riffSize >> 24) & 0xFF)
        return Data(out)
    }
}
