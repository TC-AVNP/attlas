import type {
  Page,
  PageSummary,
  JournalEntry,
  JournalSummary,
} from "./types";

const API_PREFIX = "/homelab-planner/api";

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
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  // Wiki pages
  listPages: () => request<{ pages: PageSummary[] }>("/pages"),
  createPage: (body: { slug: string; title: string; body: string }) =>
    request<Page>("/pages", { method: "POST", body: JSON.stringify(body) }),
  getPage: (slug: string) => request<Page>(`/pages/${slug}`),
  updatePage: (slug: string, body: { title?: string; body?: string }) =>
    request<Page>(`/pages/${slug}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  // Journal
  listJournal: () => request<{ entries: JournalSummary[] }>("/journal"),
  getJournalEntry: (id: number) => request<JournalEntry>(`/journal/${id}`),
  createJournalEntry: (body: { date: string; title: string; body: string }) =>
    request<JournalEntry>("/journal", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateJournalEntry: (
    id: number,
    body: { date?: string; title?: string; body?: string },
  ) =>
    request<JournalEntry>(`/journal/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteJournalEntry: (id: number) =>
    request<void>(`/journal/${id}`, { method: "DELETE" }),
};
