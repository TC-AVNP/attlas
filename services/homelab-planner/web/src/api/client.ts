import type {
  ChecklistItem,
  ItemOption,
  ItemStatus,
  BuildLogEntry,
  Step,
  StepCategory,
  StepDetail,
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
  // Steps
  listSteps: () => request<{ steps: Step[] }>("/steps"),
  getStep: (id: number) => request<StepDetail>(`/steps/${id}`),
  createStep: (body: {
    title: string;
    description?: string;
    category?: StepCategory;
    total_budget_cents?: number;
  }) =>
    request<Step>("/steps", { method: "POST", body: JSON.stringify(body) }),
  updateStep: (
    id: number,
    body: {
      title?: string;
      description?: string;
      position?: number;
      total_budget_cents?: number;
      completed?: boolean;
    },
  ) =>
    request<Step>(`/steps/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteStep: (id: number) =>
    request<void>(`/steps/${id}`, { method: "DELETE" }),

  // Checklist items
  createItem: (
    stepId: number,
    body: { name: string; group_name?: string; budget_cents?: number },
  ) =>
    request<ChecklistItem>(`/steps/${stepId}/items`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateItem: (
    id: number,
    body: {
      name?: string;
      group_name?: string;
      budget_cents?: number;
      actual_cost_cents?: number;
      status?: ItemStatus;
      selected_option_id?: number;
    },
  ) =>
    request<ChecklistItem>(`/items/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteItem: (id: number) =>
    request<void>(`/items/${id}`, { method: "DELETE" }),

  // Item options
  createOption: (
    itemId: number,
    body: { name: string; url?: string; price_cents?: number; notes?: string },
  ) =>
    request<ItemOption>(`/items/${itemId}/options`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateOption: (
    id: number,
    body: {
      name?: string;
      url?: string;
      price_cents?: number;
      notes?: string;
    },
  ) =>
    request<ItemOption>(`/options/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteOption: (id: number) =>
    request<void>(`/options/${id}`, { method: "DELETE" }),

  // Build log
  createLogEntry: (stepId: number, body: { body: string }) =>
    request<BuildLogEntry>(`/steps/${stepId}/log`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateLogEntry: (id: number, body: { body?: string }) =>
    request<BuildLogEntry>(`/log/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteLogEntry: (id: number) =>
    request<void>(`/log/${id}`, { method: "DELETE" }),
};
