import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import type { Release } from "@/api/client";
import { renderApp, testUser } from "@/test-utils";

const rel = (over: Partial<Release>): Release => ({
  guid: "g", title: "Some.Release", indexerId: 1, indexerName: "Alpha", protocol: "torrent", categories: [2040], flags: [],
  ...over,
});

function sse(events: [string, unknown][]) {
  return events.map(([e, d]) => `event: ${e}\ndata: ${JSON.stringify(d)}\n\n`).join("");
}

/** A fetch whose /search/stream answer arrives in two chunks, the second when release() is called. */
function mockStream(chunks: string[]) {
  let release: () => void = () => undefined;
  const second = new Promise<void>((r) => (release = r));
  const calls: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    calls.push(url.pathname + url.search);
    const enc = new TextEncoder();
    const body = new ReadableStream<Uint8Array>({
      async start(c) {
        c.enqueue(enc.encode(": search started\n\n" + chunks[0]));
        await second;
        c.enqueue(enc.encode(chunks[1]));
        c.close();
      },
    });
    return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
  }));
  return { calls, release: () => release() };
}

describe("search page", () => {
  it("shows results as indexers answer, sorts them and keeps grab disabled", async () => {
    const first = sse([["indexer", { indexer: { indexerId: 1, indexerName: "Alpha", status: "ok", count: 2, cached: false, durationMs: 40 },
      releases: [rel({ guid: "a", title: "Small.Release", size: 1000, seeders: 5, downloadLink: "/api/v1/indexers/1/download?token=x",
        magnetUrl: "magnet:?xt=urn:btih:abc", flags: ["freeleech"] }),
      rel({ guid: "b", title: "Big.Release", size: 9_000_000_000, seeders: 50 })] }]]);
    const second = sse([
      ["indexer", { indexer: { indexerId: 2, indexerName: "Beta", status: "error", count: 0, cached: false, durationMs: 900,
        errorCode: "unreachable", error: "timeout" }, releases: [] }],
      ["done", { total: 2, durationMs: 950 }],
    ]);
    const s = mockStream([first, second]);
    renderApp("/search", { me: testUser() });
    await userEvent.type(await screen.findByLabelText("Search for"), "release");
    await userEvent.selectOptions(screen.getByLabelText("Kind"), "movie");
    await userEvent.click(screen.getByRole("button", { name: "Movies" }));
    await userEvent.click(screen.getByRole("button", { name: "Search" }));

    const table = await screen.findByTestId("search-results");
    expect(within(table).getByText("Big.Release")).toBeInTheDocument();
    expect(screen.queryByTestId("search-summary")).not.toBeInTheDocument();
    expect(s.calls[0]).toBe("/api/v1/search/stream?q=release&type=movie&categories=2000");
    // Sorted by seeders, highest first.
    let rows = within(table).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Big.Release");
    await userEvent.click(within(table).getByRole("button", { name: "Size" }));
    await userEvent.click(within(table).getByRole("button", { name: "Size" }));
    rows = within(table).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Small.Release");
    expect(within(table).getByText("Freeleech")).toBeInTheDocument();
    expect(within(table).getAllByRole("link", { name: "Download file" })).toHaveLength(1);
    for (const b of within(table).getAllByRole("button", { name: "Grab" })) expect(b).toBeDisabled();
    expect(within(table).getAllByTitle("Available after download clients (P3)")).toHaveLength(2);

    s.release();
    expect(await screen.findByTestId("search-summary")).toHaveTextContent("2 results in 950 ms");
    expect(within(screen.getByTestId("search-indexers")).getByText("Beta")).toBeInTheDocument();
    expect(within(screen.getByTestId("search-indexers")).getByText("Error")).toBeInTheDocument();
  });

  it("hides download links from requesters", async () => {
    const s = mockStream([sse([["indexer", { indexer: { indexerId: 1, indexerName: "Alpha", status: "ok", count: 1, cached: false, durationMs: 1 },
      releases: [rel({ guid: "a", title: "One", downloadLink: "/api/v1/indexers/1/download?token=x" })] }]]), sse([["done", { total: 1, durationMs: 2 }]])]);
    s.release();
    renderApp("/search", { me: testUser({ role: "requester" }) });
    await userEvent.click(await screen.findByRole("button", { name: "Search" }));
    const table = await screen.findByTestId("search-results");
    expect(within(table).queryByRole("link", { name: "Download file" })).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("search-summary")).toBeInTheDocument());
  });
});
