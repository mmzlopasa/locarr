import SwiftUI

/// Switches between a few views of one screen. Up to three options: the system segmented
/// control. More (or accessibility text sizes, where segments truncate): a row of compact
/// chips that scrolls sideways. Both start at the list's 16 pt margin.
struct SegmentPicker<Value: Hashable>: View {
    let options: [Value]
    @Binding var selection: Value
    var label: String = ""
    let title: (Value) -> String
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        if options.count <= 3 && !typeSize.isAccessibilitySize {
            Picker(selection: $selection) {
                ForEach(options, id: \.self) { Text(verbatim: title($0)).tag($0) }
            } label: { Text(verbatim: label) }
            .pickerStyle(.segmented)
            .padding(.horizontal, Theme.spacing)
        } else {
            ChipRow(options: options, selection: $selection, title: title)
        }
    }
}

/// Compact chips for picking one value. A newly selected chip is scrolled into view only as
/// far as needed, so while the chips fit the row stays on the 16 pt margin.
struct ChipRow<Value: Hashable>: View {
    let options: [Value]
    @Binding var selection: Value
    let title: (Value) -> String

    var body: some View {
        ScrollViewReader { proxy in
            ChipScroller {
                ForEach(options, id: \.self) { value in
                    Chip(text: title(value), selected: selection == value) { selection = value }
                        .id(value)
                }
            }
            .onChange(of: selection) { _, value in
                withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(value) }
            }
            .onAppear { if selection != options.first { proxy.scrollTo(selection) } }
        }
    }
}

/// A sideways-scrolling row of chips: 16 pt leading margin like the list, sized to its
/// content (a horizontal scroll view otherwise grows to fill the height it is offered),
/// with soft fades at the edges where more chips are hidden, so the row reads as scrollable.
struct ChipScroller<Content: View>: View {
    @ViewBuilder let content: Content
    private let fade: CGFloat = 24
    @State private var hiddenLeading = false
    @State private var hiddenTrailing = true

    var body: some View {
        let row = ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) { content }
                .padding(.horizontal, Theme.spacing)
                .padding(.vertical, 2)
        }
        .fixedSize(horizontal: false, vertical: true)
        .mask {
            HStack(spacing: 0) {
                edge(.trailing, active: hiddenLeading)
                Color.black
                edge(.leading, active: hiddenTrailing)
            }
            .animation(.easeOut(duration: 0.15), value: hiddenLeading)
            .animation(.easeOut(duration: 0.15), value: hiddenTrailing)
        }
        if #available(iOS 18.0, *) {
            row.onScrollGeometryChange(for: [Bool].self) { geo in
                [geo.contentOffset.x > 1, geo.contentOffset.x + geo.containerSize.width < geo.contentSize.width - 1]
            } action: { _, edges in
                hiddenLeading = edges[0]
                hiddenTrailing = edges[1]
            }
        } else {
            row
        }
    }

    /// A soft fade from opaque (towards `opaque`) to clear, eased so there is no visible cut;
    /// fully opaque while nothing is hidden on that side.
    private func edge(_ opaque: UnitPoint, active: Bool) -> some View {
        let start: UnitPoint = opaque == .leading ? .leading : .trailing
        let end: UnitPoint = opaque == .leading ? .trailing : .leading
        return LinearGradient(stops: [
            .init(color: .black, location: 0),
            .init(color: .black.opacity(0.75), location: 0.35),
            .init(color: .black.opacity(0.3), location: 0.7),
            .init(color: .black.opacity(0), location: 1),
        ], startPoint: start, endPoint: end)
        .overlay(Color.black.opacity(active ? 0 : 1))
        .frame(width: fade)
    }
}

/// A selectable filter chip: compact (footnote, about 28 pt tall at the default size);
/// the selected one is filled with the accent and uses its dark ink for contrast.
struct Chip: View {
    let text: String
    var selected = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(verbatim: text)
                .font(.footnote.weight(.semibold))
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(selected ? Theme.accentInk : Theme.text)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(selected ? Theme.accent : Theme.surface2, in: Capsule())
                .overlay(Capsule().stroke(selected ? Color.clear : Theme.border))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .dynamicTypeSize(...DynamicTypeSize.accessibility2)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A secondary filter under the segment control ("Show: Everything"): a label and a menu,
/// on the content margins, so a second row of chips is not needed.
struct FilterMenu<Value: Hashable>: View {
    let label: String
    let options: [Value]
    @Binding var selection: Value
    let title: (Value) -> String

    var body: some View {
        HStack(spacing: 8) {
            Text(verbatim: label).foregroundStyle(Theme.textMuted)
            Spacer(minLength: 8)
            Menu {
                Picker(selection: $selection) {
                    ForEach(options, id: \.self) { Text(verbatim: title($0)).tag($0) }
                } label: { Text(verbatim: label) }
                .pickerStyle(.inline)
            } label: {
                HStack(spacing: 4) {
                    Text(verbatim: title(selection)).lineLimit(1)
                    Image(systemName: "chevron.up.chevron.down").font(.caption2.weight(.semibold))
                }
                .foregroundStyle(Theme.accent)
            }
        }
        .font(.subheadline)
        .accessibilityElement(children: .contain)
    }
}

/// The segment control of a screen that hosts several lists (Activity, Library on iPhone),
/// handed down to whichever list is showing. The list puts it in its first row, so it is the
/// list (not a view laid over it) that sits under the navigation bar: the large title shows
/// and collapses on scroll, and nothing pushes the content down.
struct ScreenHeader: @unchecked Sendable {
    let title: String
    let content: AnyView
}

private struct ScreenHeaderKey: EnvironmentKey {
    static let defaultValue: ScreenHeader? = nil
}

extension EnvironmentValues {
    var screenHeader: ScreenHeader? {
        get { self[ScreenHeaderKey.self] }
        set { self[ScreenHeaderKey.self] = newValue }
    }
}

/// The first row of a hosted list: the host's segment control, edge to edge, no separator.
/// Shows nothing when the list is its own screen (iPad sidebar, a pushed section).
struct ScreenHeaderRow: View {
    @Environment(\.screenHeader) private var header

    var body: some View {
        if let header {
            header.content.segmentRow()
        }
    }
}

extension View {
    /// Hands a segment control to the lists below, which also take the host's title.
    func screenHeader<Content: View>(title: String, @ViewBuilder _ content: () -> Content) -> some View {
        environment(\.screenHeader, ScreenHeader(title: title, content: AnyView(content())))
    }

    /// The navigation title, or the host's when the list is shown under a segment control.
    func screenTitle(_ title: String) -> some View {
        modifier(ScreenTitle(title: title))
    }

    /// A list row for a segment or chip control: no insets (the control has its own 16 pt
    /// margin), no background, no separator.
    func segmentRow() -> some View {
        self.listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 8, trailing: 0))
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
    }

    /// A list row on the same 16 pt margins as the title and the segment control.
    func marginRow(vertical: CGFloat = 8) -> some View {
        self.listRowInsets(EdgeInsets(top: vertical, leading: Theme.spacing, bottom: vertical, trailing: Theme.spacing))
    }

    /// Explanatory text or a filter under the segment control: margins, no background or separator.
    func noteRow() -> some View {
        self.marginRow(vertical: 4)
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
    }
}

private struct ScreenTitle: ViewModifier {
    let title: String
    @Environment(\.screenHeader) private var header

    func body(content: Content) -> some View {
        content.navigationTitle(Text(verbatim: header?.title ?? title))
    }
}
