import SwiftUI

/// One value loaded from the server, with its loading and error state.
@MainActor
@Observable
final class Loader<T: Sendable> {
    var value: T?
    var error: String?
    var loading = false

    func load(_ op: @Sendable () async throws -> T) async {
        loading = true
        defer { loading = false }
        do {
            value = try await op()
            error = nil
        } catch is CancellationError {
        } catch {
            let text = error.userMessage
            if !text.isEmpty { self.error = text }
        }
    }
}

/// Shows a loader's value, or loading / error states (with retry).
struct LoadView<T: Sendable, Content: View>: View {
    let loader: Loader<T>
    let reload: () async -> Void
    @ViewBuilder let content: (T) -> Content

    var body: some View {
        if let value = loader.value {
            content(value)
        } else if let error = loader.error {
            ErrorState(message: error) { await reload() }
        } else {
            LoadingState()
        }
    }
}

/// A list loaded page by page (offset/limit); the next page loads when the last row shows.
@MainActor
@Observable
final class Pager<Item: Sendable & Identifiable> {
    var items: [Item] = []
    var error: String?
    var loading = false
    var hasMore = true
    var total: Int?
    private var generation = 0
    let fetch: @Sendable (_ offset: Int) async throws -> (items: [Item], total: Int?, hasMore: Bool?)

    init(fetch: @escaping @Sendable (_ offset: Int) async throws -> (items: [Item], total: Int?, hasMore: Bool?)) {
        self.fetch = fetch
    }

    var loadedOnce: Bool { !items.isEmpty || (!hasMore && error == nil) }

    func reload() async {
        generation += 1
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let page = try await fetch(0)
            guard gen == generation else { return }
            items = page.items
            total = page.total
            hasMore = page.hasMore ?? (page.total.map { page.items.count < $0 } ?? !page.items.isEmpty)
            error = nil
        } catch is CancellationError {
        } catch {
            guard gen == generation else { return }
            let text = error.userMessage
            if !text.isEmpty { self.error = text }
            hasMore = false
        }
    }

    func loadMore(after item: Item) async {
        guard hasMore, !loading, item.id == items.last?.id else { return }
        let gen = generation
        loading = true
        defer { loading = false }
        do {
            let page = try await fetch(items.count)
            guard gen == generation else { return }
            let known = Set(items.map(\.id))
            items += page.items.filter { !known.contains($0.id) }
            total = page.total ?? total
            hasMore = page.hasMore ?? (page.total.map { items.count < $0 } ?? !page.items.isEmpty)
        } catch {
            hasMore = false
        }
    }
}

/// Footer row of a paged list: a spinner while more is loading.
struct PagerFooter<Item: Sendable & Identifiable>: View {
    let pager: Pager<Item>
    var emptyText: String
    var body: some View {
        if pager.items.isEmpty {
            if let error = pager.error {
                ErrorState(message: error) { await pager.reload() }
            } else if pager.loading || pager.hasMore {
                LoadingState()
            } else {
                EmptyState(text: emptyText)
            }
        } else if pager.hasMore {
            ProgressView().tint(Theme.accent).frame(maxWidth: .infinity).padding().stateRow()
        }
    }
}
