import SwiftUI

/// The notification centre: everything the server has for this account, newest first.
struct InboxView: View {
    @Environment(Session.self) private var session
    @State private var list = Loader<NotificationList>()

    var body: some View {
        List {
            if let error = list.error, list.value == nil {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else if let value = list.value {
                if value.items.isEmpty {
                    EmptyState(text: L("notifications.none"), systemImage: "bell.slash").listRowBackground(Color.clear)
                }
                ForEach(value.items) { n in
                    Button { Task { await open(n) } } label: { row(n) }
                        .buttonStyle(.plain)
                        .listRowBackground(n.read == true ? Theme.background : Theme.surface)
                        .swipeActions {
                            if n.read != true {
                                Button { Task { await markRead([n.id]) } } label: {
                                    Label { Text(verbatim: L("inbox.markRead")) } icon: { Image(systemName: "envelope.open") }
                                }
                                .tint(Theme.accent)
                            }
                        }
                }
                Section {
                    NavigationLink(value: Route.section(.settings)) {
                        Label { Text(verbatim: L("inbox.howItWorks")) } icon: { Image(systemName: "info.circle") }
                    }
                    .font(.footnote)
                }
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("notifications.title")))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if (list.value?.unread ?? 0) > 0 {
                    Button(L("notifications.markAll")) { Task { await markRead(nil) } }
                }
            }
        }
        .screenStyle()
    }

    private func row(_ n: AppNotification) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: icon(n.type))
                .font(.body.weight(.semibold))
                .foregroundStyle(tone(n.type))
                .frame(width: 32, height: 32)
                .background(Theme.surface3, in: Circle())
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: Describe.notification(n)).font(.callout).foregroundStyle(Theme.text).fixedSize(horizontal: false, vertical: true)
                Text(verbatim: Fmt.relative(n.createdAt)).font(.caption2).foregroundStyle(Theme.textFaint)
            }
            Spacer(minLength: 0)
            if n.read != true {
                Circle().fill(Theme.accent).frame(width: 8, height: 8).padding(.top, 6)
                    .accessibilityLabel(Text(verbatim: L("inbox.unread")))
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    private func icon(_ type: String) -> String {
        switch type {
        case "request.available": return "checkmark.seal"
        case "request.partial": return "circle.lefthalf.filled"
        case "request.approved": return "hand.thumbsup"
        case "request.declined": return "hand.thumbsdown"
        case "request.failed": return "exclamationmark.triangle"
        case "request.comment", "issue.comment": return "text.bubble"
        case "request.created": return "tray.and.arrow.down"
        case "issue.resolved": return "checkmark.bubble"
        default: return type.hasPrefix("issue.") ? "exclamationmark.bubble" : "bell"
        }
    }

    private func tone(_ type: String) -> Color {
        switch type {
        case "request.available", "issue.resolved", "request.approved": return Theme.success
        case "request.declined", "request.failed": return Theme.danger
        case "issue.created", "issue.reopened": return Theme.warning
        default: return Theme.info
        }
    }

    private func load() async {
        let client = session.client
        await list.load { try await client.notifications(limit: 100) }
        if let unread = list.value?.unread { await session.setUnread(unread) }
    }

    private func markRead(_ ids: [Int64]?) async {
        guard let count = try? await session.client.markRead(ids: ids, all: ids == nil ? true : nil) else { return }
        await session.setUnread(count.unread)
        await load()
    }

    private func open(_ n: AppNotification) async {
        if n.read != true { await markRead([n.id]) }
        session.open(DeepLink.forNotification(n))
    }
}
