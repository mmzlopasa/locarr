import SwiftUI

struct IssueRow: View {
    let issue: Issue
    @Environment(Session.self) private var session

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Poster(url: issue.kind == "album" ? ImageURL.cover(session.server.url, issue.poster, size: "250") : ImageURL.tmdb(session.server.url, issue.poster),
                   title: issue.title, square: issue.kind == "album", width: 50)
            VStack(alignment: .leading, spacing: 5) {
                Text(verbatim: issue.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
                Text(verbatim: issue.message).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                HStack(spacing: 6) {
                    Pill(text: L("issues.status.\(issue.status)"), tone: issue.status == "open" ? "warning" : "success")
                    Pill(text: Loc.render("issues.types.\(issue.type)", [:], fallback: issue.type))
                    if let s = issue.seasonNumber {
                        Text(verbatim: L("issues.seasonEpisode", ["s": String(s), "e": issue.episodeNumber.map { String($0) } ?? "–"]))
                            .font(.caption2.monospaced()).foregroundStyle(Theme.textMuted)
                    }
                    Spacer()
                    Text(verbatim: Fmt.relative(issue.createdAt)).font(.caption2).foregroundStyle(Theme.textFaint)
                }
            }
        }
        .card(padding: 10)
        .accessibilityElement(children: .combine)
    }
}

/// Reported problems: mine, or everyone's for those who manage requests.
struct IssuesView: View {
    @Environment(Session.self) private var session
    @State private var filter = "open"
    @State private var pager: Pager<Issue>?

    var body: some View {
        List {
            SegmentPicker(options: ["open", "resolved", "all"], selection: $filter, label: L("issues.title")) { L("issues.filters.\($0)") }
                .segmentRow()
            Text(verbatim: L(session.canManageRequests ? "issues.subtitleAll" : "issues.subtitleMine")).font(.footnote).foregroundStyle(Theme.textMuted)
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
            if let pager {
                ForEach(pager.items) { i in
                    NavigationLink(value: Route.issue(i.id)) { IssueRow(issue: i) }
                        .listRowBackground(Color.clear)
                        .listRowSeparator(.hidden)
                        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
                        .task { await pager.loadMore(after: i) }
                }
                PagerFooter(pager: pager, emptyText: L("issues.none"))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.plain)
        .refreshable { await pager?.reload() }
        .task(id: filter) {
            let client = session.client, status = filter == "all" ? nil : filter
            let p = Pager<Issue> { offset in
                let list = try await client.issues(status: status, offset: offset)
                return (list.items, list.total, nil)
            }
            pager = p
            await p.reload()
        }
        .navigationTitle(Text(verbatim: L("issues.title")))
        .screenStyle()
    }
}

struct IssueDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var issue = Loader<Issue>()
    @State private var resolving = false
    @State private var searchReplacement = false
    @State private var note = ""
    @State private var toast: String?

    var body: some View {
        ScrollView {
            LoadView(loader: issue, reload: load) { i in
                VStack(alignment: .leading, spacing: 16) {
                    IssueRow(issue: i)
                    Text(verbatim: i.message).font(.body).foregroundStyle(Theme.text).card()
                    if let by = i.reportedBy { InfoRow(label: L("issues.reportedBy"), value: by.username) }
                    if let by = i.resolvedBy {
                        Text(verbatim: L("issues.resolvedBy", ["user": by.username, "when": Fmt.date(i.resolvedAt)])
                             + (i.replacementSearched == true ? " · " + L("issues.replacementSearched") : ""))
                            .font(.footnote).foregroundStyle(Theme.textMuted)
                    }
                    if session.canManageRequests {
                        if i.status == "open" {
                            Button { resolving = true } label: { Text(verbatim: L("issues.resolve")) }.buttonStyle(PrimaryButtonStyle())
                        } else {
                            Button { Task { await reopen() } } label: { Text(verbatim: L("issues.reopen")) }.buttonStyle(SecondaryButtonStyle())
                        }
                    }
                    if session.canManageLibrary, let ref = i.libraryRef, let link = DeepLink.forItemRef(ref), let route = Route(link) {
                        NavigationLink(value: route) {
                            Label { Text(verbatim: L("title.openLibrary")) } icon: { Image(systemName: "square.stack.3d.up") }
                        }
                        .buttonStyle(SecondaryButtonStyle())
                    }
                    CommentsSection(load: { [client = session.client] in try await client.issueComments(i.id) },
                                    add: { [client = session.client] text in try await client.addIssueComment(i.id, text) })
                }
                .padding()
            }
        }
        .refreshable { await load() }
        .task { if issue.value == nil { await load() } }
        .navigationTitle(Text(verbatim: issue.value?.title ?? L("issues.title")))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .sheet(isPresented: $resolving) {
            NavigationStack {
                Form {
                    Toggle(isOn: $searchReplacement) {
                        VStack(alignment: .leading) {
                            Text(verbatim: L("issues.searchReplacement"))
                            Text(verbatim: L("issues.searchReplacementHint")).font(.caption).foregroundStyle(Theme.textMuted)
                        }
                    }
                    TextField(text: $note, prompt: Text(verbatim: L("issues.resolveNote")), axis: .vertical) { Text(verbatim: L("issues.resolveNote")) }
                }
                .scrollContentBackground(.hidden)
                .background(Theme.background)
                .navigationTitle(Text(verbatim: L("issues.resolve")))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button(L("common.cancel")) { resolving = false } }
                    ToolbarItem(placement: .confirmationAction) { Button(L("issues.resolve")) { Task { await resolve() } } }
                }
            }
            .presentationDetents([.medium])
        }
        .toast($toast)
    }

    private func load() async {
        let client = session.client, id = id
        await issue.load { try await client.issue(id) }
    }

    private func resolve() async {
        do {
            issue.value = try await session.client.resolveIssue(id, searchReplacement: searchReplacement, comment: note.isEmpty ? nil : note)
            resolving = false
            toast = L("common.saved")
        } catch { toast = error.userMessage }
    }

    private func reopen() async {
        do {
            issue.value = try await session.client.reopenIssue(id)
        } catch { toast = error.userMessage }
    }
}
