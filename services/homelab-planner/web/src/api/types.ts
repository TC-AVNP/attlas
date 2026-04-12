export type ItemStatus = "researching" | "ordered" | "arrived";

export interface Step {
  id: number;
  title: string;
  description: string;
  position: number;
  total_budget_cents?: number;
  created_at: number;
  completed_at?: number;
  item_count: number;
  arrived_count: number;
  budget_cents: number;
  actual_cents: number;
}

export interface ChecklistItem {
  id: number;
  step_id: number;
  name: string;
  group_name: string;
  budget_cents?: number;
  actual_cost_cents?: number;
  status: ItemStatus;
  selected_option_id?: number;
  created_at: number;
  options?: ItemOption[];
}

export interface ItemOption {
  id: number;
  item_id: number;
  name: string;
  url: string;
  price_cents?: number;
  notes: string;
  created_at: number;
}

export interface BuildLogEntry {
  id: number;
  step_id: number;
  body: string;
  created_at: number;
}

export interface StepDetail extends Step {
  items: ChecklistItem[];
  build_log: BuildLogEntry[];
}
