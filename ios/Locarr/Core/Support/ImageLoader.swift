import CryptoKit
import Foundation
import ImageIO
import UIKit

/// Posters and covers come from the server's image cache and need the session token, so
/// AsyncImage cannot load them. Images are kept in memory (decoded, downsampled) and on disk
/// in Caches (the original bytes, at most ~300 MB, oldest removed first).
actor ImageLoader {
    static let shared = ImageLoader()

    private let memory = NSCache<NSString, UIImage>()
    private let folder: URL
    private var inFlight: [String: Task<UIImage?, Never>] = [:]
    private var writes = 0

    init() {
        memory.countLimit = 400
        folder = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appending(path: "images", directoryHint: .isDirectory)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    }

    func image(url: URL, token: String?, maxPixel: CGFloat) async -> UIImage? {
        let key = "\(url.absoluteString)#\(Int(maxPixel))"
        if let hit = memory.object(forKey: key as NSString) { return hit }
        if let running = inFlight[key] { return await running.value }
        let file = folder.appending(path: Self.hash(url.absoluteString))
        let task = Task<UIImage?, Never> {
            if let data = try? Data(contentsOf: file), let img = Self.downsample(data, maxPixel: maxPixel) { return img }
            var req = URLRequest(url: url)
            if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
            guard let (data, resp) = try? await APIClient.defaultSession.data(for: req),
                  (resp as? HTTPURLResponse)?.statusCode == 200,
                  let img = Self.downsample(data, maxPixel: maxPixel) else { return nil }
            try? data.write(to: file, options: .atomic)
            return img
        }
        inFlight[key] = task
        let img = await task.value
        inFlight[key] = nil
        if let img { memory.setObject(img, forKey: key as NSString) }
        writes += 1
        if writes % 50 == 0 { trim() }
        return img
    }

    private static func hash(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private static func downsample(_ data: Data, maxPixel: CGFloat) -> UIImage? {
        guard let src = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary) else { return nil }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixel,
        ]
        guard let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { return nil }
        return UIImage(cgImage: cg)
    }

    /// Keeps the disk cache under its limit.
    private func trim(limit: Int = 300 * 1024 * 1024) {
        let keys: [URLResourceKey] = [.fileSizeKey, .contentModificationDateKey]
        guard let files = try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys) else { return }
        var entries = files.compactMap { url -> (URL, Int, Date)? in
            guard let v = try? url.resourceValues(forKeys: Set(keys)) else { return nil }
            return (url, v.fileSize ?? 0, v.contentModificationDate ?? .distantPast)
        }
        var total = entries.reduce(0) { $0 + $1.1 }
        guard total > limit else { return }
        entries.sort { $0.2 < $1.2 }
        for (url, size, _) in entries where total > limit {
            try? FileManager.default.removeItem(at: url)
            total -= size
        }
    }

    func clear() {
        memory.removeAllObjects()
        try? FileManager.default.removeItem(at: folder)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    }
}

/// Image URLs on a server.
enum ImageURL {
    /// A TMDb poster, backdrop or profile ("w342", "w780", …).
    static func tmdb(_ base: URL, _ file: String?, size: String = "w342") -> URL? {
        guard let file, !file.isEmpty else { return nil }
        return APIClient(baseURL: base, token: nil).url("/images/tmdb/\(size)/\(seg(file))")
    }

    /// A Cover Art Archive cover ("250", "500", "1200").
    static func cover(_ base: URL, _ file: String?, size: String = "500") -> URL? {
        guard let file, !file.isEmpty else { return nil }
        return APIClient(baseURL: base, token: nil).url("/images/coverart/\(size)/\(seg(file))")
    }

    /// The right image for a discover item: covers for music, TMDb otherwise.
    static func item(_ base: URL, _ item: DiscoverItem, large: Bool = false) -> URL? {
        item.mediaType.isMusic ? cover(base, item.poster, size: large ? "500" : "250") : tmdb(base, item.poster, size: large ? "w500" : "w342")
    }
}
