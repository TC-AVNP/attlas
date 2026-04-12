package service

import "errors"

// ItemStatus tracks where a checklist item is in the procurement flow.
type ItemStatus string

const (
	StatusResearching ItemStatus = "researching"
	StatusOrdered     ItemStatus = "ordered"
	StatusArrived     ItemStatus = "arrived"
)

func ValidItemStatus(s ItemStatus) bool {
	switch s {
	case StatusResearching, StatusOrdered, StatusArrived:
		return true
	}
	return false
}

// Step is an independent weekend-sized milestone.
type Step struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Position         int64  `json:"position"`
	TotalBudgetCents *int64 `json:"total_budget_cents,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	CompletedAt      *int64 `json:"completed_at,omitempty"`

	// Aggregates
	ItemCount    int   `json:"item_count"`
	ArrivedCount int   `json:"arrived_count"`
	BudgetCents  int64 `json:"budget_cents"`
	ActualCents  int64 `json:"actual_cents"`
}

// ChecklistItem is something to buy or do within a step.
type ChecklistItem struct {
	ID               int64      `json:"id"`
	StepID           int64      `json:"step_id"`
	Name             string     `json:"name"`
	GroupName        string     `json:"group_name"`
	BudgetCents      *int64     `json:"budget_cents,omitempty"`
	ActualCostCents  *int64     `json:"actual_cost_cents,omitempty"`
	Status           ItemStatus `json:"status"`
	SelectedOptionID *int64     `json:"selected_option_id,omitempty"`
	CreatedAt        int64      `json:"created_at"`

	Options []ItemOption `json:"options,omitempty"`
}

// ItemOption is one alternative to compare for a checklist item.
type ItemOption struct {
	ID         int64  `json:"id"`
	ItemID     int64  `json:"item_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	PriceCents *int64 `json:"price_cents,omitempty"`
	Notes      string `json:"notes"`
	CreatedAt  int64  `json:"created_at"`
}

// BuildLogEntry is a timestamped journal note for a step.
type BuildLogEntry struct {
	ID        int64  `json:"id"`
	StepID    int64  `json:"step_id"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}

// StepDetail bundles a step with its checklist items and build log.
type StepDetail struct {
	Step
	Items    []ChecklistItem `json:"items"`
	BuildLog []BuildLogEntry `json:"build_log"`
}

// Input types

type CreateStepInput struct {
	Title            string
	Description      string
	TotalBudgetCents *int64
}

type UpdateStepInput struct {
	Title            *string
	Description      *string
	Position         *int64
	TotalBudgetCents *int64
	Completed        *bool
}

type CreateItemInput struct {
	Name        string
	GroupName   string
	BudgetCents *int64
}

type UpdateItemInput struct {
	Name             *string
	GroupName        *string
	BudgetCents      *int64
	ActualCostCents  *int64
	Status           *ItemStatus
	SelectedOptionID *int64
}

type CreateOptionInput struct {
	Name       string
	URL        string
	PriceCents *int64
	Notes      string
}

type UpdateOptionInput struct {
	Name       *string
	URL        *string
	PriceCents *int64
	Notes      *string
}

type CreateLogEntryInput struct {
	Body string
}

type UpdateLogEntryInput struct {
	Body *string
}

// Sentinel errors
var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
)
