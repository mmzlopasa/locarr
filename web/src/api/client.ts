import createClient from "openapi-fetch";
import type { components, paths } from "./schema.gen";

type Schemas = components["schemas"];
export type SystemInfo = Schemas["SystemInfo"];
export type SystemStatus = Schemas["SystemStatus"];
export type User = Schemas["User"];
export type Role = Schemas["Role"];
export type Language = Schemas["Language"];
export type Permissions = Schemas["Permissions"];
export type Session = Schemas["Session"];
export type ApiKey = Schemas["ApiKey"];
export type CreatedApiKey = Schemas["CreatedApiKey"];
export type Job = Schemas["Job"];
export type JobState = Schemas["JobState"];
export type ScheduledTask = Schemas["ScheduledTask"];
export type TaskList = Schemas["TaskList"];
export type Settings = Schemas["Settings"];
export type RootFolder = Schemas["RootFolder"];
export type MediaKind = Schemas["MediaKind"];
export type FolderListing = Schemas["FolderListing"];
export type TotpSetup = Schemas["TotpSetup"];
export type DownloadProtocol = Schemas["DownloadProtocol"];
export type DownloadState = Schemas["DownloadState"];
export type DownloadStatus = Schemas["DownloadStatus"];
export type QueuePriority = Schemas["QueuePriority"];
export type DownloadClientField = Schemas["DownloadClientField"];
export type DownloadClientImplementation = Schemas["DownloadClientImplementation"];
export type DownloadClient = Schemas["DownloadClient"];
export type DownloadClientInput = Schemas["DownloadClientInput"];
export type DownloadClientStatus = Schemas["DownloadClientStatus"];
export type DownloadClientTestResult = Schemas["DownloadClientTestResult"];
export type DownloadSettings = Schemas["DownloadSettings"];
export type RemotePathMapping = Schemas["RemotePathMapping"];
export type QueueItem = Schemas["QueueItem"];
export type QueueClient = Schemas["QueueClient"];
export type Queue = Schemas["Queue"];
export type BlocklistEntry = Schemas["BlocklistEntry"];
export type Indexer = Schemas["Indexer"];
export type IndexerInput = Schemas["IndexerInput"];
export type IndexerCaps = Schemas["IndexerCaps"];
export type IndexerCategories = Schemas["IndexerCategories"];
export type IndexerTestResult = Schemas["IndexerTestResult"];
export type IndexerSettings = Schemas["IndexerSettings"];
export type IndexerSource = Schemas["IndexerSource"];
export type IndexerSourceKind = Schemas["IndexerSourceKind"];
export type IndexerSourceItem = Schemas["IndexerSourceItem"];
export type Release = Schemas["Release"];
export type SearchType = Schemas["SearchType"];
export type SearchIndexerResult = Schemas["SearchIndexerResult"];
export type SearchStreamIndexerEvent = Schemas["SearchStreamIndexerEvent"];
export type SearchStreamDone = Schemas["SearchStreamDone"];
export type Quality = Schemas["Quality"];
export type QualityDefinition = Schemas["QualityDefinition"];
export type QualityProfile = Schemas["QualityProfile"];
export type QualityProfileInput = Schemas["QualityProfileInput"];
export type QualityProfileItem = Schemas["QualityProfileItem"];
export type CustomFormat = Schemas["CustomFormat"];
export type CustomFormatInput = Schemas["CustomFormatInput"];
export type CustomFormatSpec = Schemas["CustomFormatSpec"];
export type CustomFormatSchema = Schemas["CustomFormatSchema"];
export type CustomFormatImplementation = Schemas["CustomFormatImplementation"];
export type ReleaseProfile = Schemas["ReleaseProfile"];
export type ReleaseProfileInput = Schemas["ReleaseProfileInput"];
export type DelayProfile = Schemas["DelayProfile"];
export type DelayProfileInput = Schemas["DelayProfileInput"];
export type LanguageInfo = Schemas["LanguageInfo"];
export type ParseRequest = Schemas["ParseRequest"];
export type ParseResult = Schemas["ParseResult"];
export type ParsedRelease = Schemas["ParsedRelease"];
export type Decision = Schemas["Decision"];
export type Rejection = Schemas["Rejection"];
type ErrorBody = Schemas["Error"];

/** Typed API client generated from api/openapi.yaml (see scripts/gen-api.sh). */
export const api = createClient<paths>({
  // Absolute same-origin URL: identical in browsers, and valid for Request() in tests.
  baseUrl: new URL("/api/v1", globalThis.location?.origin ?? "http://localhost").href,
  credentials: "same-origin",
  // Resolve fetch per call (not at import) so tests can replace it.
  fetch: (req: Request) => globalThis.fetch(req),
});

/** An error response from the API; `code` is stable and used for translations. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryAfter?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** Returns data or throws ApiError for non-2xx responses. */
export function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.response.ok) return res.data as T;
  const body = res.error as Partial<ErrorBody> | undefined;
  const retry = res.response.headers.get("Retry-After");
  throw new ApiError(
    res.response.status,
    body?.code ?? `http_${res.response.status}`,
    body?.message ?? res.response.statusText,
    retry ? Number(retry) : undefined,
  );
}

/** Error codes that a 401 can carry without meaning "your session is gone". */
const CREDENTIAL_CODES = new Set(["invalid_credentials", "totp_required", "invalid_totp", "bad_setup_code", "account_locked"]);

/** Reports an error that means the caller is no longer signed in. */
export function isSessionLost(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401 && !CREDENTIAL_CODES.has(err.code);
}

export async function fetchSystemInfo(): Promise<SystemInfo> {
  return unwrap(await api.GET("/system/info"));
}

/** The signed-in user, or null when there is no valid session. */
export async function fetchMe(): Promise<User | null> {
  const res = await api.GET("/auth/me");
  if (res.response.status === 401) return null;
  return unwrap(res);
}

/** Describes this browser for the device list, e.g. "Firefox · Linux". */
export function deviceInfo() {
  const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
  const browser = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome"
    : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /iPhone|iPad/.test(ua) ? "iOS" : /Android/.test(ua) ? "Android" : /Mac OS X/.test(ua) ? "macOS"
    : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return { name: os ? `${browser} · ${os}` : browser, client: "Locarr Web" };
}

export const queryKeys = {
  info: ["system", "info"] as const,
  me: ["auth", "me"] as const,
  status: ["system", "status"] as const,
  tasks: ["system", "tasks"] as const,
  users: ["users"] as const,
  sessions: ["auth", "sessions"] as const,
  apiKeys: ["auth", "apikeys"] as const,
  settings: ["settings"] as const,
  rootFolders: ["rootfolders"] as const,
  folders: (path: string) => ["filesystem", path] as const,
  qualities: ["qualities"] as const,
  qualityDefinitions: ["qualitydefinitions"] as const,
  qualityProfiles: ["qualityprofiles"] as const,
  customFormats: ["customformats"] as const,
  customFormatSchema: ["customformats", "schema"] as const,
  releaseProfiles: ["releaseprofiles"] as const,
  delayProfiles: ["delayprofiles"] as const,
  languages: ["languages"] as const,
  downloadClients: ["downloadclients"] as const,
  downloadImplementations: ["downloadclients", "implementations"] as const,
  downloadSettings: ["downloadclients", "settings"] as const,
  remotePathMappings: ["remotepathmappings"] as const,
  queue: ["queue"] as const,
  blocklist: ["queue", "blocklist"] as const,
  indexers: ["indexers"] as const,
  indexerSources: ["indexers", "sources"] as const,
  indexerSettings: ["indexers", "settings"] as const,
};
