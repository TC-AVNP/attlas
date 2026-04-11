// Thin fetch wrapper that prepends /petboard/api and throws on non-2xx
// responses. Every page that talks to the server goes through here so
// the URL prefix and error handling live in exactly one place.
//
// We deliberately do NOT depend on `import.meta.env.BASE_URL` because
// the API is always served from /petboard/api regardless of how the SPA
// itself is mounted (Vite's `base` only affects asset URLs).

const API_PREFIX = "/petboard/api";

export class ApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(`api ${status}: ${body || "(empty body)"}`);
    this.status = status;
    this.body = body;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_PREFIX}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    ...init,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new ApiError(res.status, text);
  }
  // 204 No Content has no body to parse.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

import type { ListProjectsResponse, ProjectDetail } from "./types";

export const api = {
  listProjects: () => request<ListProjectsResponse>("/projects"),
  getProject: (slug: string) => request<ProjectDetail>(`/projects/${encodeURIComponent(slug)}`),
};
