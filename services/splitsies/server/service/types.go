package service

import "errors"

// User represents a whitelisted Splitsies user.
type User struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Picture     string `json:"picture,omitempty"`
	IsAdmin     bool   `json:"is_admin"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   int64  `json:"created_at"`
	LastLoginAt *int64 `json:"last_login_at,omitempty"`
}

// Group is a set of users who split expenses together.
type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PhotoURL    string `json:"photo_url,omitempty"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
}

// GroupDetail bundles a group with its members.
type GroupDetail struct {
	Group
	Members []User `json:"members"`
}

// Category for expenses.
type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	CreatedBy *int64 `json:"created_by,omitempty"`
}

// Expense is a single cost within a group.
type Expense struct {
	ID          int64          `json:"id"`
	GroupID     int64          `json:"group_id"`
	PaidBy      int64          `json:"paid_by"`
	PaidByName  string         `json:"paid_by_name,omitempty"`
	Amount      int64          `json:"amount"` // cents
	Description string         `json:"description"`
	CategoryID  *int64         `json:"category_id,omitempty"`
	Category    string         `json:"category,omitempty"`
	SplitType   string         `json:"split_type"`
	Splits      []ExpenseSplit `json:"splits,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	DeletedAt   *int64         `json:"deleted_at,omitempty"`
}

// ExpenseSplit records how much one user owes for a given expense.
type ExpenseSplit struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name,omitempty"`
	Amount   int64  `json:"amount"` // cents
}

// Settlement records a real-world payment between two users.
type Settlement struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"group_id"`
	FromUser     int64  `json:"from_user"`
	FromUserName string `json:"from_user_name"`
	ToUser       int64  `json:"to_user"`
	ToUserName   string `json:"to_user_name"`
	Amount       int64  `json:"amount"` // cents
	CreatedAt    int64  `json:"created_at"`
	DeletedAt    *int64 `json:"deleted_at,omitempty"`
}

// Balance between two users, optionally broken down by group.
type PairBalance struct {
	UserID   int64          `json:"user_id"`
	UserName string         `json:"user_name"`
	Net      int64          `json:"net"` // positive = they owe you
	Groups   []GroupBalance `json:"groups,omitempty"`
}

// GroupBalance is the net within a single group between two users.
type GroupBalance struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	Net       int64  `json:"net"`
}

// SuggestedPayment is one step in the minimum-payments settlement plan.
type SuggestedPayment struct {
	FromUser     int64  `json:"from_user"`
	FromUserName string `json:"from_user_name"`
	ToUser       int64  `json:"to_user"`
	ToUserName   string `json:"to_user_name"`
	Amount       int64  `json:"amount"`
}

// MonthSummary for the spending overview.
type MonthSummary struct {
	Month      string              `json:"month"` // "2026-04"
	Total      int64               `json:"total"`
	ByGroup    []GroupSpend        `json:"by_group"`
	ByCategory []CategorySpend    `json:"by_category,omitempty"`
}

type GroupSpend struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	Total     int64  `json:"total"`
}

type CategorySpend struct {
	CategoryID   int64  `json:"category_id"`
	CategoryName string `json:"category_name"`
	Total        int64  `json:"total"`
}

// TimelineEntry is either an expense or settlement in the chronological view.
type TimelineEntry struct {
	Type       string  `json:"type"` // "expense" or "settlement"
	ID         int64   `json:"id"`
	Amount     int64   `json:"amount"`
	Description string `json:"description"`
	Category   string  `json:"category,omitempty"`
	PaidByName string  `json:"paid_by_name,omitempty"`
	FromName   string  `json:"from_name,omitempty"`
	ToName     string  `json:"to_name,omitempty"`
	SplitType  string  `json:"split_type,omitempty"`
	CreatedAt  int64   `json:"created_at"`
}

// --- Input types ---

type CreateGroupInput struct {
	Name        string
	Description string
	PhotoURL    string
}

type AddExpenseInput struct {
	GroupID     int64
	PaidBy     int64
	Amount     int64
	Description string
	CategoryID *int64
	SplitType  string
	Splits     []SplitInput
}

type SplitInput struct {
	UserID int64
	Amount int64 // cents for custom, basis points (hundredths of %) for percentage
}

type AddSettlementInput struct {
	GroupID  int64
	FromUser int64
	ToUser   int64
	Amount   int64
}

// Sentinel errors.
var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("forbidden")
	ErrUnauthorized = errors.New("unauthorized")
)
