import SwiftUI

/// The brand mark (the web favicon): an L with a dot, mint on dark.
struct BrandMark: View {
    var size: CGFloat = 64
    var body: some View {
        Canvas { ctx, sz in
            let s = sz.width / 32
            ctx.fill(Path(roundedRect: CGRect(origin: .zero, size: sz), cornerRadius: 7 * s), with: .color(Color(hex: 0x12161C)))
            var p = Path()
            p.move(to: CGPoint(x: 10 * s, y: 7 * s))
            p.addLine(to: CGPoint(x: 10 * s, y: 25 * s))
            p.addLine(to: CGPoint(x: 22 * s, y: 25 * s))
            ctx.stroke(p, with: .color(Theme.accent), style: StrokeStyle(lineWidth: 3.5 * s, lineCap: .round, lineJoin: .round))
            ctx.fill(Path(ellipseIn: CGRect(x: 18 * s, y: 8 * s, width: 6 * s, height: 6 * s)), with: .color(Theme.accent))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// First screen: the address of a Locarr server.
struct ConnectView: View {
    @Environment(AppModel.self) private var app
    var onCancel: (() -> Void)? = nil
    @State private var address = ""
    @State private var checking = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                BrandMark(size: 72).padding(.top, 40)
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: L("connect.title")).font(.largeTitle.weight(.bold)).foregroundStyle(Theme.text)
                    Text(verbatim: L("connect.subtitle")).font(.body).foregroundStyle(Theme.textMuted)
                }
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: L("connect.address")).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.textMuted)
                    TextField(text: $address, prompt: Text(verbatim: "192.168.1.10:8484")) { Text(verbatim: L("connect.address")) }
                        .textContentType(.URL)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.go)
                        .focused($focused)
                        .onSubmit { Task { await check() } }
                        .fieldStyle()
                    Text(verbatim: L("connect.addressHint")).font(.footnote).foregroundStyle(Theme.textFaint)
                }
                if let error { Callout(text: error, tone: "danger") }
                Button {
                    Task { await check() }
                } label: {
                    if checking { ProgressView().tint(Theme.accentInk) } else { Text(verbatim: L("connect.continue")) }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(address.trimmingCharacters(in: .whitespaces).isEmpty || checking)
                if let onCancel {
                    Button(L("common.cancel"), action: onCancel).frame(maxWidth: .infinity).foregroundStyle(Theme.textMuted)
                }
            }
            .padding(24)
            .frame(maxWidth: 520)
            .frame(maxWidth: .infinity)
        }
        .background(Theme.background.ignoresSafeArea())
        .onAppear { focused = true }
    }

    private func check() async {
        error = nil
        guard let url = ServerURL.normalize(address) else {
            error = L("connect.invalid")
            return
        }
        guard ServerURL.isAllowed(url) else {
            error = L("connect.needsHttps")
            return
        }
        checking = true
        defer { checking = false }
        do {
            let info = try await app.anonymousClient(url).systemInfo()
            guard info.product.lowercased() == "locarr" else {
                error = L("connect.notLocarr")
                return
            }
            if info.setupRequired == true {
                error = L("connect.setupRequired")
                return
            }
            let server = app.addServer(url: url, name: info.instanceName ?? "")
            app.open(server)
            onCancel?()
        } catch let e as APIError where e.code == "insecure" {
            error = L("connect.needsHttps")
        } catch let e as APIError where e.code == "decoding" || e.status == 404 {
            error = L("connect.notLocarr")
        } catch {
            self.error = L("connect.unreachable") + " " + error.userMessage
        }
    }
}

extension View {
    /// A text field on a raised surface.
    func fieldStyle() -> some View {
        self.padding(.horizontal, 14)
            .frame(minHeight: 48)
            .background(Theme.surface2, in: RoundedRectangle(cornerRadius: Theme.radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).stroke(Theme.border))
            .foregroundStyle(Theme.text)
    }
}

/// Sign in to one server: Locarr account or a password provider (Streamero), then the
/// two-factor code when the account asks for one.
struct SignInView: View {
    @Environment(AppModel.self) private var app
    let server: SavedServer
    var notice: String?
    @State private var providers: [AuthProvider] = []
    @State private var method: SignInMethod = .local
    @State private var username = ""
    @State private var password = ""
    @State private var code = ""
    @State private var useRecovery = false
    @State private var needsCode = false
    @State private var busy = false
    @State private var error: String?
    @State private var switching = false
    @FocusState private var field: Field?

    enum Field { case username, password, code }

    private var providerName: String {
        if case .provider(_, let name) = method { return name }
        return ""
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                HStack(spacing: 14) {
                    BrandMark(size: 48)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: server.name).font(.headline).foregroundStyle(Theme.text)
                        Text(verbatim: server.url.absoluteString).font(.footnote.monospaced()).foregroundStyle(Theme.textMuted).lineLimit(1)
                    }
                }
                .padding(.top, 32)
                Text(verbatim: L("login.title")).font(.largeTitle.weight(.bold)).foregroundStyle(Theme.text)
                if let notice { Callout(text: notice, tone: "warning") }
                Text(verbatim: subtitle).foregroundStyle(Theme.textMuted)

                if !providers.isEmpty && !needsCode {
                    Picker(selection: $method) {
                        Text(verbatim: L("login.localAccount")).tag(SignInMethod.local)
                        ForEach(providers) { p in Text(verbatim: p.name).tag(SignInMethod.provider(id: p.id, name: p.name)) }
                    } label: { Text(verbatim: L("login.method")) }
                    .pickerStyle(.segmented)
                }

                if needsCode {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField(text: $code, prompt: Text(verbatim: useRecovery ? "xxxx-xxxx" : "123456")) {
                            Text(verbatim: codeLabel)
                        }
                        .textContentType(.oneTimeCode)
                        .keyboardType(useRecovery ? .asciiCapable : .numberPad)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($field, equals: .code)
                        .fieldStyle()
                        .accessibilityLabel(Text(verbatim: codeLabel))
                        if method == .local {
                            Button(L(useRecovery ? "login.useTotp" : "login.useRecovery")) { useRecovery.toggle(); code = "" }
                                .font(.footnote).foregroundStyle(Theme.info)
                        }
                    }
                } else {
                    TextField(text: $username, prompt: Text(verbatim: L("login.username"))) { Text(verbatim: L("login.username")) }
                        .textContentType(.username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($field, equals: .username)
                        .submitLabel(.next)
                        .onSubmit { field = .password }
                        .fieldStyle()
                    SecureField(text: $password, prompt: Text(verbatim: L("login.password"))) { Text(verbatim: L("login.password")) }
                        .textContentType(.password)
                        .focused($field, equals: .password)
                        .submitLabel(.go)
                        .onSubmit { Task { await submit() } }
                        .fieldStyle()
                }
                if let error { Callout(text: error, tone: "danger") }
                Button {
                    Task { await submit() }
                } label: {
                    if busy { ProgressView().tint(Theme.accentInk) } else { Text(verbatim: buttonTitle) }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(busy || (needsCode ? code.isEmpty : username.isEmpty || password.isEmpty))

                if needsCode {
                    Button(L("common.back")) { needsCode = false; code = ""; error = nil }
                        .frame(maxWidth: .infinity).foregroundStyle(Theme.textMuted)
                }
                Divider().overlay(Theme.border).padding(.vertical, 6)
                Button { switching = true } label: {
                    Label { Text(verbatim: L("servers.other")) } icon: { Image(systemName: "server.rack") }
                }
                .foregroundStyle(Theme.textMuted)
            }
            .padding(24)
            .frame(maxWidth: 520)
            .frame(maxWidth: .infinity)
        }
        .background(Theme.background.ignoresSafeArea())
        .task {
            if let all = try? await app.anonymousClient(server.url).providers() {
                providers = SignIn.passwordProviders(all)
            }
            if let name = server.username, username.isEmpty { username = name }
            field = username.isEmpty ? .username : .password
        }
        .onChange(of: method) { _, _ in error = nil }
        .sheet(isPresented: $switching) { NavigationStack { ServerListView() } }
    }

    private var subtitle: String {
        if needsCode {
            return method == .local ? L("login.totpHint") : L("login.providerTotpHint", ["name": providerName])
        }
        return method == .local ? L("login.subtitle") : L("login.providerSubtitle", ["name": providerName])
    }

    private var codeLabel: String {
        if method != .local { return L("login.streameroCode") }
        return L(useRecovery ? "login.recoveryCode" : "login.totpCode")
    }

    private var buttonTitle: String {
        if needsCode { return L("login.verify") }
        return method == .local ? L("login.submit") : L("login.submitWith", ["name": providerName])
    }

    private func submit() async {
        busy = true
        defer { busy = false }
        error = nil
        let outcome = await SignIn.attempt(client: app.anonymousClient(server.url), method: method, username: username, password: password,
                                           code: useRecovery ? nil : code, recoveryCode: useRecovery ? code : nil, device: AppModel.device)
        switch outcome {
        case .signedIn(let token, let user):
            app.signedIn(server, token: token, user: user)
        case .needsCode:
            if needsCode { error = L("errors.invalid_totp") }
            needsCode = true
            field = .code
        case .failed(let e):
            error = e.localizedText
            if e.code == "invalid_totp" { code = "" }
        }
    }
}
