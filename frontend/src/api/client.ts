// Single fetch wrapper used by every feature module.
//
//   - credentials: 'include' so the session cookie rides on every call.
//   - X-Requested-With header satisfies the CSRF check the backend
//     applies to mutating endpoints (Phase 2).
//   - Throws ApiError on non-2xx so TanStack Query routes them to onError.
//
// In dev Vite proxies /api → backend, so we use a relative base by default;
// VITE_API_BASE overrides for prod or cross-origin testing.

export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

const BASE = import.meta.env.VITE_API_BASE ?? "";

type Json = Record<string, unknown> | unknown[] | string | number | boolean | null;

interface RequestOpts {
  method?: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  body?: Json;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

export async function api<T = unknown>(path: string, opts: RequestOpts = {}): Promise<T> {
  const url = path.startsWith("http") ? path : `${BASE}${path}`;
  const res = await fetch(url, {
    method: opts.method ?? "GET",
    credentials: "include",
    signal: opts.signal,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "fetch",
      ...(opts.headers ?? {}),
    },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  const text = await res.text();
  let parsed: unknown = undefined;
  if (text.length > 0) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = text;
    }
  }

  if (!res.ok) {
    const msg = typeof parsed === "object" && parsed && "error" in parsed
      ? String((parsed as { error: unknown }).error)
      : `HTTP ${res.status}`;
    throw new ApiError(res.status, msg, parsed);
  }

  return parsed as T;
}
