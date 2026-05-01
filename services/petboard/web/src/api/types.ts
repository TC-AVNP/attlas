// TypeScript mirrors of the Go service types in
// services/petboard/server/service/types.go. Keep these in sync — there
// is no codegen yet, so any field added on the server needs to be
// reflected here by hand.

export type Priority = "high" | "medium" | "low";

export type Stage = "idea" | "live" | "completed";

export type Interest = "excited" | "meh" | "bored";

export type Status = "backlog" | "in_progress" | "done" | "dropped";

export interface Project {
  id: number;
  slug: string;
  name: string;
  problem: string;
  description?: string;
  priority: Priority;
  stage: Stage;
  interest: Interest;
  color: string;
  created_at: number; // unix seconds
  archived_at?: number;
  canvas_x?: number;
  canvas_y?: number;

  // Aggregates — only populated on list/get responses.
  feature_counts?: Partial<Record<Status, number>>;
  total_minutes: number;
}

export interface Feature {
  id: number;
  project_id: number;
  title: string;
  description?: string;
  status: Status;
  created_at: number;
  started_at?: number;
  completed_at?: number;
  dropped_at?: number;
}

export interface EffortLog {
  id: number;
  project_id: number;
  feature_id?: number;
  minutes: number;
  note?: string;
  logged_at: number;
}

export interface ProjectDetail extends Project {
  features: Feature[];
  effort: EffortLog[];
}

export interface ListProjectsResponse {
  projects: Project[];
}

// Todo is a standalone reminder that isn't tied to any project. Used
// for cross-cutting work like "refactor the foo package" — things you
// want to remember but don't deserve their own project.
export interface Todo {
  id: number;
  text: string;
  created_at: number;
  completed_at?: number;
}

export interface ListTodosResponse {
  todos: Todo[];
}
