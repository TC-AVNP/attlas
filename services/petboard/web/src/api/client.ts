// Thin fetch wrapper that prepends /petboard/api and throws on non-2xx
// responses. Every page that talks to the server goes through here so
// the URL prefix and error handling live in exactly one place.
//
// We deliberately do NOT depend on `import.meta.env.BASE_URL` because
// the API is always served from /petboard/api regardless of how the SPA
// itself is mounted (Vite's `base` only affects asset URLs).

import type {
  EffortLog,
  Feature,
  ListProjectsResponse,
  ListTodosResponse,
  Priority,
  Project,
  ProjectDetail,
  Status,
  Todo,
} from "./types";

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

export interface CreateProjectBody {
  name: string;
  problem: string;
  priority: Priority;
  description?: string;
  color?: string;
}

export interface UpdateProjectBody {
  name?: string;
  problem?: string;
  description?: string;
  priority?: Priority;
  color?: string;
  canvas_x?: number;
  canvas_y?: number;
  archived?: boolean;
}

export interface CreateFeatureBody {
  title: string;
  description?: string;
}

export interface UpdateFeatureBody {
  title?: string;
  description?: string;
  status?: Status;
}

export interface LogEffortBody {
  minutes: number;
  note?: string;
  feature_id?: number;
}

export const api = {
  // Projects
  listProjects: (includeArchived = false) =>
    request<ListProjectsResponse>(
      `/projects${includeArchived ? "?include_archived=1" : ""}`,
    ),
  getProject: (slug: string) =>
    request<ProjectDetail>(`/projects/${encodeURIComponent(slug)}`),
  createProject: (body: CreateProjectBody) =>
    request<Project>("/projects", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateProject: (slug: string, body: UpdateProjectBody) =>
    request<ProjectDetail>(`/projects/${encodeURIComponent(slug)}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  // Features (nested create, top-level update/delete)
  createFeature: (slug: string, body: CreateFeatureBody) =>
    request<Feature>(`/projects/${encodeURIComponent(slug)}/features`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateFeature: (id: number, body: UpdateFeatureBody) =>
    request<Feature>(`/features/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteFeature: (id: number) =>
    request<void>(`/features/${id}`, { method: "DELETE" }),

  // Effort
  logEffort: (slug: string, body: LogEffortBody) =>
    request<EffortLog>(`/projects/${encodeURIComponent(slug)}/effort`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  // Standalone todos (not tied to any project)
  listTodos: (includeCompleted = false) =>
    request<ListTodosResponse>(
      `/todos${includeCompleted ? "?include_completed=1" : ""}`,
    ),
  createTodo: (text: string) =>
    request<Todo>("/todos", {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
  updateTodo: (id: number, body: { text?: string; completed?: boolean }) =>
    request<Todo>(`/todos/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteTodo: (id: number) =>
    request<void>(`/todos/${id}`, { method: "DELETE" }),
};
