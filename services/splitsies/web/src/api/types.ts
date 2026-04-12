export interface User {
  id: number;
  email: string;
  name: string;
  picture?: string;
  is_admin: boolean;
  is_active: boolean;
  created_at: number;
  last_login_at?: number;
}

export interface Group {
  id: number;
  name: string;
  description: string;
  photo_url?: string;
  created_by: number;
  created_at: number;
  members: User[];
}

export interface Category {
  id: number;
  name: string;
  is_default: boolean;
  created_by?: number;
}

export interface ExpenseSplit {
  id: number;
  user_id: number;
  user_name: string;
  amount: number;
}

export interface Expense {
  id: number;
  group_id: number;
  paid_by: number;
  paid_by_name: string;
  amount: number;
  description: string;
  category_id?: number;
  category?: string;
  split_type: string;
  splits: ExpenseSplit[];
  created_at: number;
}

export interface Settlement {
  id: number;
  group_id: number;
  from_user: number;
  from_user_name: string;
  to_user: number;
  to_user_name: string;
  amount: number;
  created_at: number;
}

export interface PairBalance {
  user_id: number;
  user_name: string;
  net: number;
  groups: GroupBalance[];
}

export interface GroupBalance {
  group_id: number;
  group_name: string;
  net: number;
}

export interface BalancesResponse {
  total_net: number;
  balances: PairBalance[];
}

export interface SuggestedPayment {
  from_user: number;
  from_user_name: string;
  to_user: number;
  to_user_name: string;
  amount: number;
}

export interface TimelineEntry {
  type: "expense" | "settlement";
  id: number;
  amount: number;
  description: string;
  category?: string;
  paid_by_name?: string;
  from_name?: string;
  to_name?: string;
  split_type?: string;
  created_at: number;
}

export interface MonthSummary {
  month: string;
  total: number;
  by_group: GroupSpend[];
  by_category?: CategorySpend[];
}

export interface GroupSpend {
  group_id: number;
  group_name: string;
  total: number;
}

export interface CategorySpend {
  category_id: number;
  category_name: string;
  total: number;
}

export interface MemberBalance {
  user_id: number;
  user_name: string;
  net: number;
}
