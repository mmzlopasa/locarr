import { useEffect, useLayoutEffect, useRef, useState } from "react";

/** Rows to render for a scroll position (with overscan rows on both sides). */
export function visibleRows(scrollTop: number, viewport: number, rowHeight: number, rowCount: number, overscan = 3) {
  if (rowCount <= 0 || rowHeight <= 0) return { first: 0, last: -1 };
  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const last = Math.min(rowCount - 1, Math.ceil((scrollTop + viewport) / rowHeight) + overscan);
  return { first: Math.min(first, last), last };
}

/** Pages (of pageSize items) holding items first..last. */
export function pagesFor(firstItem: number, lastItem: number, pageSize: number): number[] {
  if (lastItem < firstItem) return [0];
  const out: number[] = [];
  for (let p = Math.floor(firstItem / pageSize); p <= Math.floor(lastItem / pageSize); p++) out.push(p);
  return out;
}

/** Grid columns for a width (at least one). */
export function columnsFor(width: number, minCard: number, gap: number): number {
  return Math.max(1, Math.floor((width + gap) / (minCard + gap)));
}

/** Tracks an element's scroll position, height and width. */
export function useScrollBox<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [box, setBox] = useState({ scrollTop: 0, height: 800, width: 1000 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setBox({ scrollTop: el.scrollTop, height: el.clientHeight || 800, width: el.clientWidth || 1000 });
  }, []);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() =>
        setBox({ scrollTop: el.scrollTop, height: el.clientHeight || 800, width: el.clientWidth || 1000 }));
    };
    el.addEventListener("scroll", update, { passive: true });
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(update) : null;
    ro?.observe(el);
    return () => {
      cancelAnimationFrame(frame);
      el.removeEventListener("scroll", update);
      ro?.disconnect();
    };
  }, []);
  return { ref, ...box };
}
