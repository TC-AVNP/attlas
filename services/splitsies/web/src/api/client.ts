import type {
  BalancesResponse,
  Category,
  Expense,
  Group,
  MemberBalance,
  MonthSummary,
  Settlement,
  SuggestedPayment,
  TimelineEntry,
  User,
} from "./types";

const API_PREFIX = "/api";

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
  // Auth
  me: () => request<User>("/auth/me"),
  logout: () => request<void>("/auth/logout", { method: "POST" }),

  // Users
  listUsers: () => request<User[]>("/users"),
  addUser: (email: string, is_admin: boolean) =>
    request<User>("/users", {
      method: "POST",
      body: JSON.stringify({ email, is_admin }),
    }),
  removeUser: (id: number) =>
    request<void>(`/users/${id}`, { method: "DELETE" }),

  // Groups
  listGroups: () => request<Group[]>("/groups"),
  getGroup: (id: number) => request<Group>(`/groups/${id}`),
  createGroup: (name: string, description: string, photo_url: string) =>
    request<Group>("/groups", {
      method: "POST",
      body: JSON.stringify({ name, description, photo_url }),
    }),
  addGroupMember: (groupId: number, userId: number) =>
    request<Group>(`/groups/${groupId}/members`, {
      method: "POST",
      body: JSON.stringify({ user_id: userId }),
    }),

  // Categories
  listCategories: () => request<Category[]>("/categories"),
  createCategory: (name: string) =>
    request<Category>("/categories", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),

  // Expenses
  listExpenses: (groupId: number, category?: string, search?: string) => {
    const params = new URLSearchParams();
    if (category) params.set("category", category);
    if (search) params.set("search", search);
    const qs = params.toString();
    return request<Expense[]>(
      `/groups/${groupId}/expenses${qs ? "?" + qs : ""}`,
    );
  },
  addExpense: (
    groupId: number,
    body: {
      paid_by: number;
      amount: number;
      description: string;
      category_id?: number;
      split_type: string;
      splits?: { user_id: number; amount: number }[];
    },
  ) =>
    request<Expense>(`/groups/${groupId}/expenses`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  deleteExpense: (id: number) =>
    request<void>(`/expenses/${id}`, { method: "DELETE" }),

  // Settlements
  addSettlement: (
    groupId: number,
    from_user: number,
    to_user: number,
    amount: number,
  ) =>
    request<Settlement>(`/groups/${groupId}/settlements`, {
      method: "POST",
      body: JSON.stringify({ from_user, to_user, amount }),
    }),
  deleteSettlement: (id: number) =>
    request<void>(`/settlements/${id}`, { method: "DELETE" }),

  // Balances
  getMyBalances: () => request<BalancesResponse>("/balances"),
  getGroupBalances: (groupId: number) =>
    request<MemberBalance[]>(`/groups/${groupId}/balances`),
  suggestPayments: (groupId: number) =>
    request<SuggestedPayment[]>(`/groups/${groupId}/suggested-payments`),

  // Timeline
  getTimeline: (groupId: number, category?: string, search?: string) => {
    const params = new URLSearchParams();
    if (category) params.set("category", category);
    if (search) params.set("search", search);
    const qs = params.toString();
    return request<TimelineEntry[]>(
      `/groups/${groupId}/timeline${qs ? "?" + qs : ""}`,
    );
  },

  // Overview
  getOverview: () => request<MonthSummary[]>("/overview"),
  getOverviewMonth: (month: string) =>
    request<MonthSummary>(`/overview/${month}`),
};
