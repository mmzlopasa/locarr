import Foundation

/// How the user signs in: a Locarr account or a password provider such as Streamero.
enum SignInMethod: Hashable, Sendable {
    case local
    case provider(id: String, name: String)
}

enum SignInOutcome: Sendable, Equatable {
    case signedIn(token: String, user: User)
    /// The password was right; ask for the authenticator code and send everything again.
    case needsCode
    case failed(APIError)
}

/// The sign-in steps against `/auth/login` and `/auth/providers/{id}/login`
/// (401 `totp_required` → the same request again with the code).
enum SignIn {
    static func attempt(client: APIClient, method: SignInMethod, username: String, password: String,
                        code: String?, recoveryCode: String? = nil, device: DeviceInfo) async -> SignInOutcome {
        let code = code?.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: " ", with: "")
        do {
            let result: AuthResult
            switch method {
            case .local:
                result = try await client.login(LoginRequest(username: username, password: password,
                                                             totpCode: code?.isEmpty == false ? code : nil,
                                                             recoveryCode: recoveryCode?.isEmpty == false ? recoveryCode : nil,
                                                             device: device))
            case .provider(let id, _):
                result = try await client.loginWithProvider(id, ProviderLoginRequest(username: username, password: password,
                                                                                    totpCode: code?.isEmpty == false ? code : nil,
                                                                                    device: device))
            }
            guard let token = result.token, !token.isEmpty else {
                return .failed(APIError(status: 200, code: "unknown", message: "no token in the answer"))
            }
            return .signedIn(token: token, user: result.user)
        } catch let error as APIError {
            if error.code == "totp_required" { return .needsCode }
            return .failed(error)
        } catch {
            return .failed(APIError.network(String(describing: error)))
        }
    }

    /// Password providers the server offers (redirect providers need a browser and are left out).
    static func passwordProviders(_ all: [AuthProvider]) -> [AuthProvider] {
        all.filter { $0.kind == "password" && $0.id != "local" }
    }
}
