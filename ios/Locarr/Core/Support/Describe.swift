import Foundation

/// Server answers in the reader's language, with the server's English text as the fallback
/// (the same keys and rules as the web client).
enum Describe {
    private static func str(_ v: JSONValue?) -> String { v?.text ?? "" }

    private static func when(_ v: JSONValue?) -> String {
        let s = str(v)
        return s.isEmpty ? L("explain.unknownTime") : Fmt.dateTime(s)
    }

    private static func percent(_ v: JSONValue?) -> String {
        String(Int(((v?.number ?? 0) * 100).rounded()))
    }

    static func notification(_ n: AppNotification) -> String {
        let vars = ["title": n.title, "actor": n.actor?.username ?? "", "message": n.message ?? ""]
        let base = Loc.render("notifications.types.\(n.type.replacingOccurrences(of: ".", with: "_"))", vars, fallback: n.title)
        if let m = n.message, !m.isEmpty, n.type != "request.comment", n.type != "issue.comment" {
            return base + " " + L("notifications.detail", vars)
        }
        return base
    }

    static func reason(_ r: ReasonCount) -> String {
        Loc.render("rejectionCodes.\(r.code)", [:], fallback: r.example ?? r.code)
    }

    static func rejection(_ r: Rejection) -> String {
        Loc.render("rejectionCodes.\(r.code)", [:], fallback: r.message ?? r.code)
    }

    static func downloadState(_ s: String?) -> String {
        guard let s, !s.isEmpty else { return "" }
        return Loc.render("downloadStates.\(s)", [:], fallback: s)
    }

    /// "Why isn't it here?"
    static func explanation(_ ex: Explanation) -> String {
        let p = ex.params ?? .null
        let reasonText = ex.reasons?.first.map(reason) ?? ""
        var vars: [String: String] = [
            "progress": percent(p["progress"]),
            "title": str(p["title"]),
            "state": downloadState(str(p["state"])),
            "detail": str(p["detail"]),
            "searches": str(p["searches"]).isEmpty ? "0" : str(p["searches"]),
            "candidates": str(p["candidates"]).isEmpty ? "0" : str(p["candidates"]),
            "next": when(p["nextSearch"]),
            "from": when(p["availableFrom"]),
            "retry": when(p["retryAt"]),
            "reason": reasonText,
            "accepted": str(p["accepted"]).isEmpty ? "0" : str(p["accepted"]),
        ]
        if ex.code == "grab_failed" {
            let why = p["failCode"].isNull && p["detail"].isNull ? "" : grabFailure(code: str(p["failCode"]), error: str(p["detail"]),
                                                                                         indexer: str(p["indexer"]), until: str(p["until"]), attempts: str(p["attempts"]))
            if why.isEmpty { return L("explain.codes.grab_failed_unknown", vars) }
            vars["detail"] = why
            return L("explain.codes.grab_failed", vars)
        }
        return Loc.render("explain.codes.\(ex.code)", vars, fallback: ex.message ?? ex.code)
    }

    static func grabFailure(code: String, error: String, indexer: String, until: String, attempts: String) -> String {
        let vars = ["indexer": indexer.isEmpty ? L("timeline.grabErrors.theIndexer") : indexer, "error": error,
                    "attempts": attempts.isEmpty ? "0" : attempts, "until": until.isEmpty ? "" : Fmt.dateTime(until)]
        if code.isEmpty || code == "other" { return error }
        let key = !until.isEmpty && Loc.has("timeline.grabErrors.\(code)_until") ? "\(code)_until" : code
        return Loc.render("timeline.grabErrors.\(key)", vars, fallback: error.isEmpty ? code : error)
    }

    static func boardItem(_ item: BoardItem) -> String {
        let p = item.params ?? .null
        let vars: [String: String] = [
            "title": str(p["title"]),
            "detail": str(p["detail"]),
            "progress": percent(p["progress"]),
            "searches": str(p["searches"]).isEmpty ? String(item.searches ?? 0) : str(p["searches"]),
            "languages": str(p["languages"]),
            "next": when(p["nextSearch"]),
            "since": when(p["stalledSince"]),
            "provider": str(p["provider"]),
        ]
        if Loc.has("board.itemCodes.\(item.code)") {
            return L("board.itemCodes.\(item.code)", vars)
        }
        return explanation(Explanation(code: item.code, tone: item.tone, message: item.message, params: item.params, reasons: item.reasons))
    }

    static func boardGroupTitle(_ g: BoardGroup) -> String {
        Loc.render("board.groups.\(g.code).title", [:], fallback: g.code)
    }

    static func boardGroupHint(_ g: BoardGroup) -> String {
        guard Loc.has("board.groups.\(g.code).hint") else { return "" }
        return L("board.groups.\(g.code).hint", ["searches": String(g.searches ?? 0)])
    }

    private static let byteParams: Set<String> = ["free", "total", "min", "size", "limit", "freeBytes", "minBytes", "sizeBytes", "limitBytes", "walBytes"]
    private static let timeParams: Set<String> = ["since", "resetAt", "completedAt", "retryAt"]

    static func health(_ issue: HealthIssue) -> String {
        var vars: [String: String] = [:]
        for (k, v) in (issue.params ?? .null).object {
            if byteParams.contains(k), let n = v.number { vars[k] = Fmt.bytes(Int64(n)) }
            else if timeParams.contains(k), case .string(let s) = v { vars[k] = Fmt.dateTime(s) }
            else if k == "kind", case .string(let s) = v { vars[k] = Loc.render("mediaKinds.\(s)", [:], fallback: s) }
            else { vars[k] = v.text }
        }
        return Loc.render("health.codes.\(issue.code)", vars, fallback: issue.message ?? issue.code)
    }

    /// One line of a title's timeline.
    static func event(_ e: TimelineEvent) -> String {
        let d = e.data ?? .null
        let message = e.message ?? ""
        let summary = d["summary"]
        func last(_ path: String) -> String { String(path.split(separator: "/").last ?? "") }
        func transfer(_ v: JSONValue) -> String { Loc.render("transfer.\(str(v))", [:], fallback: str(v)) }
        switch e.type {
        case "added": return L("timeline.events.added", ["path": str(d["path"])])
        case "edited": return L("timeline.events.edited")
        case "refreshed": return L("timeline.events.refreshed")
        case "search":
            let inds = d["indexers"].array.map { i -> String in
                let status = str(i["status"])
                let value = status == "ok" ? str(i["count"]) : Loc.render("timeline.indexerStatus.\(status)", [:], fallback: status)
                return "\(str(i["name"])): \(value)"
            }.joined(separator: ", ")
            let source = str(d["source"]).isEmpty ? "automatic" : str(d["source"])
            return L("timeline.events.search", ["source": Loc.render("timeline.sources.\(source)", [:], fallback: source),
                                                "count": str(d["resultCount"]).isEmpty ? "0" : str(d["resultCount"]),
                                                "indexers": inds.isEmpty ? "–" : inds])
        case "decision":
            var text = L("timeline.events.decision", [
                "reason": Loc.render("timeline.reasons.\(str(d["reason"]))", [:], fallback: str(d["reason"])),
                "candidates": num(summary["candidates"]), "accepted": num(summary["accepted"]),
                "pending": num(summary["pending"]), "rejected": num(summary["rejected"]),
            ])
            if !str(summary["best"]).isEmpty { text += " " + L("timeline.events.best", ["title": str(summary["best"])]) }
            return text
        case "pending": return L("timeline.events.pending", ["title": str(d["title"])])
        case "grab_failed":
            let code = str(d["code"])
            if code == "attempts_exhausted" { return L("timeline.grabErrors.attempts_exhausted", ["attempts": num(d["attempts"])]) }
            let why = grabFailure(code: code, error: str(d["error"]), indexer: str(d["indexer"]), until: str(d["until"]), attempts: str(d["attempts"]))
            if d["skipped"].bool == true { return L("timeline.events.grabSkipped", ["reason": why]) }
            return L("timeline.events.grabFailed", ["title": str(d["title"]), "error": why])
        case "retry": return L("timeline.events.retry", ["title": str(d["title"]), "message": str(d["message"])])
        case "file_imported":
            if case .array(let files) = d["files"] {
                return L("timeline.events.importedTracks", ["count": String(files.count), "folder": str(d["folder"]), "mode": transfer(d["mode"])])
            }
            return L("timeline.events.imported", ["quality": str(d["quality"]), "mode": transfer(d["mode"]), "path": str(d["path"])])
        case "file_found":
            if let n = d["files"].number { return L("timeline.events.foundTracks", ["count": String(Int(n)), "path": str(d["path"])]) }
            return L("timeline.events.found", ["quality": str(d["quality"]), "path": str(d["path"])])
        case "import_failed", "download_import_failed": return L("timeline.events.importFailed", ["message": message])
        case "import_skipped":
            let fallback = L("timeline.events.importSkipped", ["message": message])
            return Loc.render("timeline.skipped.\(str(d["code"]))", ["download": str(d["download"]), "path": last(str(d["path"]))], fallback: fallback)
        case "renamed":
            if let n = d["count"].number { return L("timeline.events.renamedTracks", ["count": String(Int(n)), "example": str(d["example"])]) }
            return L("timeline.events.renamed", ["from": last(str(d["from"])), "to": last(str(d["to"]))])
        case "upgraded": return L("timeline.events.upgraded", ["from": str(d["from"]), "to": str(d["to"])])
        case "recycled":
            return d["restored"].bool == true ? L("timeline.events.restored", ["path": str(d["path"])]) : L("timeline.events.recycled", ["path": str(d["path"])])
        case "deleted": return L("timeline.events.deleted", ["message": message])
        case "notified":
            let ok = d["ok"].bool == true
            if !ok && !str(d["reason"]).isEmpty { return L("timeline.events.notifyFailedReason", ["server": str(d["server"]), "reason": str(d["reason"])]) }
            if ok && !d["libraries"].array.isEmpty { return L("timeline.events.notifiedScan", ["server": str(d["server"]), "libraries": str(d["libraries"])]) }
            return L(ok ? "timeline.events.notified" : "timeline.events.notifyFailed", ["server": str(d["server"])])
        case "download_grabbed":
            return L("timeline.events.grabbed", ["title": str(d["title"]), "indexer": str(d["indexer"]).isEmpty ? "–" : str(d["indexer"]), "client": str(d["client"])])
        case "download_progress": return L("timeline.events.progress", ["message": message])
        case "download_state":
            return L("timeline.events.state", ["from": downloadState(str(d["fromState"])), "to": downloadState(str(d["toState"]))])
        case "download_completed": return L("timeline.events.completed", ["title": str(d["title"])])
        case "download_failed": return L("timeline.events.failed", ["title": str(d["title"]), "message": message])
        case "download_removed": return L("timeline.events.removed", ["message": message])
        case "download_imported": return L("timeline.events.downloadImported", ["message": message])
        case "download_blocklisted": return L("timeline.events.blocklisted", ["title": str(d["title"])])
        case "album_added": return L("timeline.events.albumAdded", ["title": str(d["title"])])
        case "album_removed": return L("timeline.events.albumRemoved", ["message": message])
        default: return message
        }
    }

    private static func num(_ v: JSONValue) -> String { v.number.map { String(Int($0)) } ?? "0" }

    /// The tone of a timeline event (colour of its dot).
    static func eventTone(_ type: String) -> String {
        switch type {
        case "file_imported", "download_imported", "upgraded", "download_completed", "file_found", "release_matched", "cover_saved": return "success"
        case "import_failed", "import_skipped", "download_import_failed", "download_failed", "grab_failed": return "danger"
        case "pending", "retry", "download_blocklisted", "recycled", "deleted": return "warning"
        case "search", "decision", "download_grabbed", "download_progress": return "info"
        default: return "neutral"
        }
    }

    static func requestStatus(_ s: RequestStatus) -> String { Loc.render("requests.status.\(s.rawValue)", [:], fallback: s.rawValue) }
    static func discoverStatus(_ s: DiscoverStatus) -> String { Loc.render("discover.status.\(s.rawValue)", [:], fallback: s.rawValue) }

    static func requestKind(_ kind: String) -> String { Loc.render("requests.kinds.\(kind)", [:], fallback: kind) }

    /// "seasons 1, 2" / "all seasons" for a series request.
    static func requestSeasons(_ r: MediaRequest) -> String? {
        guard r.kind == "series" else { return nil }
        if r.allSeasons == true { return L(r.futureSeasons == true ? "requests.allAndFuture" : "requests.allSeasons") }
        let list = (r.seasons ?? []).map { String($0.number) }.joined(separator: ", ")
        guard !list.isEmpty else { return nil }
        var text = L("requests.seasonList", ["list": list])
        if r.futureSeasons == true { text += " " + L("requests.futureToo") }
        return text
    }
}
