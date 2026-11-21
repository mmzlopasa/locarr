import { ApiError, type SearchStreamDone, type SearchStreamIndexerEvent, type SearchType } from "@/api/client";

export interface SearchParams {
  q?: string;
  type?: SearchType;
  categories?: number[];
  indexerIds?: number[];
}

/** Builds the query string shared by /search and /search/stream. */
export function searchQuery(p: SearchParams): string {
  const qs = new URLSearchParams();
  if (p.q?.trim()) qs.set("q", p.q.trim());
  if (p.type && p.type !== "search") qs.set("type", p.type);
  if (p.categories?.length) qs.set("categories", p.categories.join(","));
  if (p.indexerIds?.length) qs.set("indexerIds", p.indexerIds.join(","));
  return qs.toString();
}

export interface StreamHandlers {
  onIndexer: (e: SearchStreamIndexerEvent) => void;
  onDone: (e: SearchStreamDone) => void;
}

/**
 * Reads the Server-Sent Events stream of /search/stream with fetch (so errors
 * keep their status and the search can be aborted). Resolves when the stream ends.
 */
export async function streamSearch(p: SearchParams, h: StreamHandlers, signal?: AbortSignal): Promise<void> {
  const url = new URL(`/api/v1/search/stream?${searchQuery(p)}`, globalThis.location?.origin ?? "http://localhost");
  const res = await globalThis.fetch(url.href, { credentials: "same-origin", signal, headers: { Accept: "text/event-stream" } });
  if (!res.ok) {
    let body: { code?: string; message?: string } = {};
    try {
      body = (await res.json()) as typeof body;
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, body.code ?? `http_${res.status}`, body.message ?? res.statusText);
  }
  if (!res.body) throw new ApiError(500, "internal", "no stream");
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += value;
    let i: number;
    while ((i = buf.indexOf("\n\n")) >= 0) {
      dispatch(buf.slice(0, i), h);
      buf = buf.slice(i + 2);
    }
  }
  if (buf.trim()) dispatch(buf, h);
}

function dispatch(block: string, h: StreamHandlers) {
  let event = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith(":")) continue;
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!data.length) return;
  const payload: unknown = JSON.parse(data.join("\n"));
  if (event === "indexer") h.onIndexer(payload as SearchStreamIndexerEvent);
  else if (event === "done") h.onDone(payload as SearchStreamDone);
}
