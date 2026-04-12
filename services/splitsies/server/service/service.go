// Package service is the business layer. Handlers delegate here.
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

type Service struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Service {
	return &Service{DB: db, Now: time.Now}
}

func (s *Service) now() int64 { return s.Now().Unix() }

// --- Auth / Sessions -------------------------------------------------------

// CreateSession generates a random token, stores its hash, and returns
// the raw token for the cookie.
func (s *Service) CreateSession(userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256Hash(token)
	now := s.now()
	expires := now + 30*24*3600 // 30 days
	_, err := s.DB.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, userID, now, expires,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ValidateSession checks the token and returns the user if valid.
func (s *Service) ValidateSession(token string) (*User, error) {
	hash := sha256Hash(token)
	var u User
	var isAdmin, isActive int
	err := s.DB.QueryRow(`
		SELECT u.id, u.email, u.name, u.picture, u.is_admin, u.is_active, u.created_at, u.last_login_at
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > ?
	`, hash, s.now()).Scan(
		&u.ID, &u.Email, &u.Name, &u.Picture, &isAdmin, &isActive, &u.CreatedAt, &u.LastLoginAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin == 1
	u.IsActive = isActive == 1
	if !u.IsActive {
		return nil, ErrForbidden
	}
	return &u, nil
}

func (s *Service) DeleteSession(token string) error {
	hash := sha256Hash(token)
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- Users -----------------------------------------------------------------

// FindOrCreateUser looks up a user by email. If found and active, updates
// their profile from Google. If found but inactive, returns ErrForbidden.
// If not found, returns ErrNotFound (they're not whitelisted).
func (s *Service) FindOrCreateUser(email, name, picture string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	var isAdmin, isActive int
	err := s.DB.QueryRow(
		`SELECT id, email, name, picture, is_admin, is_active, created_at, last_login_at FROM users WHERE email = ?`,
		email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &isAdmin, &isActive, &u.CreatedAt, &u.LastLoginAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: email %s is not whitelisted", ErrNotFound, email)
	}
	if err != nil {
		return nil, err
	}

	u.IsAdmin = isAdmin == 1
	u.IsActive = isActive == 1

	if !u.IsActive {
		return nil, ErrForbidden
	}

	// Update profile info and last login from Google
	now := s.now()
	_, err = s.DB.Exec(
		`UPDATE users SET name = ?, picture = ?, last_login_at = ? WHERE id = ?`,
		name, picture, now, u.ID,
	)
	if err != nil {
		return nil, err
	}
	u.Name = name
	u.Picture = picture
	u.LastLoginAt = &now
	return &u, nil
}

// EnsureInitialAdmin whitelists email as an admin if no admin exists yet.
// Idempotent: returns nil without changes once any admin is present, so
// the env var can stay set in the systemd unit forever.
func (s *Service) EnsureInitialAdmin(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	var adminCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_active = 1`,
	).Scan(&adminCount); err != nil {
		return err
	}
	if adminCount > 0 {
		return nil
	}
	log.Printf("splitsies: no admin exists — bootstrapping %s as initial admin", email)
	_, err := s.AddUser(email, true)
	return err
}

// AddUser whitelists an email. Called by admins.
func (s *Service) AddUser(email string, isAdmin bool) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidInput)
	}

	// Check if already exists
	var existing int64
	err := s.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&existing)
	if err == nil {
		// Re-activate if deactivated
		_, err = s.DB.Exec(`UPDATE users SET is_active = 1 WHERE id = ?`, existing)
		if err != nil {
			return nil, err
		}
		return s.getUser(existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	admin := 0
	if isAdmin {
		admin = 1
	}
	now := s.now()
	res, err := s.DB.Exec(
		`INSERT INTO users (email, is_admin, is_active, created_at) VALUES (?, ?, 1, ?)`,
		email, admin, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{
		ID:        id,
		Email:     email,
		IsAdmin:   isAdmin,
		IsActive:  true,
		CreatedAt: now,
	}, nil
}

// RemoveUser deactivates a user (revokes access but keeps history).
func (s *Service) RemoveUser(userID int64) error {
	res, err := s.DB.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	// Delete their sessions
	_, _ = s.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return nil
}

// ListUsers returns all users.
func (s *Service) ListUsers() ([]User, error) {
	rows, err := s.DB.Query(
		`SELECT id, email, name, picture, is_admin, is_active, created_at, last_login_at FROM users ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		var isAdmin, isActive int
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &isAdmin, &isActive, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		u.IsAdmin = isAdmin == 1
		u.IsActive = isActive == 1
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Service) getUser(id int64) (*User, error) {
	var u User
	var isAdmin, isActive int
	err := s.DB.QueryRow(
		`SELECT id, email, name, picture, is_admin, is_active, created_at, last_login_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &isAdmin, &isActive, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin == 1
	u.IsActive = isActive == 1
	return &u, nil
}

// --- Groups ----------------------------------------------------------------

func (s *Service) CreateGroup(creatorID int64, in CreateGroupInput) (*GroupDetail, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	now := s.now()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(
		`INSERT INTO groups (name, description, photo_url, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.Name, in.Description, in.PhotoURL, creatorID, now,
	)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	gid, _ := res.LastInsertId()

	// Creator is automatically a member.
	_, err = tx.Exec(
		`INSERT INTO group_members (group_id, user_id, added_at) VALUES (?, ?, ?)`,
		gid, creatorID, now,
	)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetGroup(gid)
}

func (s *Service) GetGroup(groupID int64) (*GroupDetail, error) {
	var g Group
	err := s.DB.QueryRow(
		`SELECT id, name, description, photo_url, created_by, created_at FROM groups WHERE id = ?`, groupID,
	).Scan(&g.ID, &g.Name, &g.Description, &g.PhotoURL, &g.CreatedBy, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	members, err := s.groupMembers(groupID)
	if err != nil {
		return nil, err
	}
	return &GroupDetail{Group: g, Members: members}, nil
}

func (s *Service) ListGroupsForUser(userID int64) ([]GroupDetail, error) {
	rows, err := s.DB.Query(`
		SELECT g.id, g.name, g.description, g.photo_url, g.created_by, g.created_at
		FROM groups g
		JOIN group_members gm ON g.id = gm.group_id
		WHERE gm.user_id = ?
		ORDER BY g.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []GroupDetail
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.PhotoURL, &g.CreatedBy, &g.CreatedAt); err != nil {
			return nil, err
		}
		members, err := s.groupMembers(g.ID)
		if err != nil {
			return nil, err
		}
		groups = append(groups, GroupDetail{Group: g, Members: members})
	}
	return groups, rows.Err()
}

func (s *Service) AddGroupMember(groupID, adderID, userID int64) error {
	// Only the group creator can add members.
	var createdBy int64
	err := s.DB.QueryRow(`SELECT created_by FROM groups WHERE id = ?`, groupID).Scan(&createdBy)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if createdBy != adderID {
		return fmt.Errorf("%w: only the group creator can add members", ErrForbidden)
	}

	// Check user exists
	var exists int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: user does not exist", ErrInvalidInput)
	}

	_, err = s.DB.Exec(
		`INSERT OR IGNORE INTO group_members (group_id, user_id, added_at) VALUES (?, ?, ?)`,
		groupID, userID, s.now(),
	)
	return err
}

func (s *Service) groupMembers(groupID int64) ([]User, error) {
	rows, err := s.DB.Query(`
		SELECT u.id, u.email, u.name, u.picture, u.is_admin, u.is_active, u.created_at, u.last_login_at
		FROM users u JOIN group_members gm ON u.id = gm.user_id
		WHERE gm.group_id = ?
		ORDER BY gm.added_at ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []User
	for rows.Next() {
		var u User
		var isAdmin, isActive int
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &isAdmin, &isActive, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		u.IsAdmin = isAdmin == 1
		u.IsActive = isActive == 1
		members = append(members, u)
	}
	return members, rows.Err()
}

func (s *Service) isGroupMember(groupID, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID,
	).Scan(&n)
	return n > 0, err
}

// --- Categories ------------------------------------------------------------

func (s *Service) ListCategories() ([]Category, error) {
	rows, err := s.DB.Query(`SELECT id, name, is_default, created_by FROM categories ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []Category
	for rows.Next() {
		var c Category
		var isDefault int
		if err := rows.Scan(&c.ID, &c.Name, &isDefault, &c.CreatedBy); err != nil {
			return nil, err
		}
		c.IsDefault = isDefault == 1
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func (s *Service) CreateCategory(name string, createdBy int64) (*Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	res, err := s.DB.Exec(
		`INSERT INTO categories (name, is_default, created_by) VALUES (?, 0, ?)`,
		strings.ToLower(name), createdBy,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: category already exists", ErrInvalidInput)
	}
	id, _ := res.LastInsertId()
	return &Category{ID: id, Name: strings.ToLower(name), IsDefault: false, CreatedBy: &createdBy}, nil
}

// --- Expenses --------------------------------------------------------------

func (s *Service) AddExpense(callerID int64, in AddExpenseInput) (*Expense, error) {
	if in.Amount <= 0 {
		return nil, fmt.Errorf("%w: amount must be positive", ErrInvalidInput)
	}
	if in.Description = strings.TrimSpace(in.Description); in.Description == "" {
		return nil, fmt.Errorf("%w: description is required", ErrInvalidInput)
	}
	if in.SplitType != "even" && in.SplitType != "custom" && in.SplitType != "percentage" {
		return nil, fmt.Errorf("%w: split_type must be even, custom, or percentage", ErrInvalidInput)
	}

	// Verify caller is a group member
	isMember, err := s.isGroupMember(in.GroupID, callerID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, fmt.Errorf("%w: not a group member", ErrForbidden)
	}

	// Verify payer is a group member
	isMember, err = s.isGroupMember(in.GroupID, in.PaidBy)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, fmt.Errorf("%w: payer is not a group member", ErrInvalidInput)
	}

	// Calculate splits
	splits := in.Splits
	switch in.SplitType {
	case "even":
		if len(splits) == 0 {
			// Default: split among all group members
			members, err := s.groupMembers(in.GroupID)
			if err != nil {
				return nil, err
			}
			splits = make([]SplitInput, len(members))
			for i, m := range members {
				splits[i] = SplitInput{UserID: m.ID}
			}
		}
		perPerson := in.Amount / int64(len(splits))
		remainder := in.Amount - perPerson*int64(len(splits))
		for i := range splits {
			splits[i].Amount = perPerson
			if int64(i) < remainder {
				splits[i].Amount++
			}
		}
	case "custom":
		var total int64
		for _, sp := range splits {
			total += sp.Amount
		}
		if total != in.Amount {
			return nil, fmt.Errorf("%w: custom split amounts must sum to the total (%d != %d)", ErrInvalidInput, total, in.Amount)
		}
	case "percentage":
		var totalBps int64
		for _, sp := range splits {
			totalBps += sp.Amount
		}
		if totalBps != 10000 {
			return nil, fmt.Errorf("%w: percentages must sum to 100%% (got %d basis points)", ErrInvalidInput, totalBps)
		}
		// Convert basis points to cents
		var allocated int64
		for i := range splits {
			splits[i].Amount = in.Amount * splits[i].Amount / 10000
			allocated += splits[i].Amount
		}
		// Distribute rounding remainder
		remainder := in.Amount - allocated
		for i := int64(0); i < remainder; i++ {
			splits[i%int64(len(splits))].Amount++
		}
	}

	now := s.now()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}

	res, err := tx.Exec(`
		INSERT INTO expenses (group_id, paid_by, amount, description, category_id, split_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, in.GroupID, in.PaidBy, in.Amount, in.Description, in.CategoryID, in.SplitType, now)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	expID, _ := res.LastInsertId()

	for _, sp := range splits {
		_, err = tx.Exec(
			`INSERT INTO expense_splits (expense_id, user_id, amount) VALUES (?, ?, ?)`,
			expID, sp.UserID, sp.Amount,
		)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.getExpense(expID)
}

func (s *Service) DeleteExpense(callerID, expenseID int64) error {
	// Verify caller is a member of the expense's group
	var groupID int64
	err := s.DB.QueryRow(`SELECT group_id FROM expenses WHERE id = ? AND deleted_at IS NULL`, expenseID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	isMember, err := s.isGroupMember(groupID, callerID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrForbidden
	}

	_, err = s.DB.Exec(`UPDATE expenses SET deleted_at = ? WHERE id = ?`, s.now(), expenseID)
	return err
}

func (s *Service) getExpense(id int64) (*Expense, error) {
	var e Expense
	var catName sql.NullString
	var payerName string
	err := s.DB.QueryRow(`
		SELECT e.id, e.group_id, e.paid_by, COALESCE(NULLIF(u.name, ''), u.email), e.amount, e.description,
		       e.category_id, COALESCE(c.name, ''), e.split_type, e.created_at, e.deleted_at
		FROM expenses e
		JOIN users u ON e.paid_by = u.id
		LEFT JOIN categories c ON e.category_id = c.id
		WHERE e.id = ?
	`, id).Scan(
		&e.ID, &e.GroupID, &e.PaidBy, &payerName, &e.Amount, &e.Description,
		&e.CategoryID, &catName, &e.SplitType, &e.CreatedAt, &e.DeletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.PaidByName = payerName
	if catName.Valid {
		e.Category = catName.String
	}

	// Load splits
	rows, err := s.DB.Query(`
		SELECT es.id, es.user_id, COALESCE(NULLIF(u.name, ''), u.email), es.amount
		FROM expense_splits es JOIN users u ON es.user_id = u.id
		WHERE es.expense_id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sp ExpenseSplit
		if err := rows.Scan(&sp.ID, &sp.UserID, &sp.UserName, &sp.Amount); err != nil {
			return nil, err
		}
		e.Splits = append(e.Splits, sp)
	}
	return &e, rows.Err()
}

func (s *Service) ListExpenses(groupID int64, category string, search string) ([]Expense, error) {
	query := `
		SELECT e.id, e.group_id, e.paid_by, COALESCE(NULLIF(u.name, ''), u.email), e.amount, e.description,
		       e.category_id, COALESCE(c.name, ''), e.split_type, e.created_at
		FROM expenses e
		JOIN users u ON e.paid_by = u.id
		LEFT JOIN categories c ON e.category_id = c.id
		WHERE e.group_id = ? AND e.deleted_at IS NULL
	`
	args := []any{groupID}

	if category != "" {
		query += ` AND c.name = ?`
		args = append(args, category)
	}
	if search != "" {
		query += ` AND e.description LIKE ?`
		args = append(args, "%"+search+"%")
	}
	query += ` ORDER BY e.created_at DESC`

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expenses []Expense
	for rows.Next() {
		var e Expense
		var catName string
		if err := rows.Scan(
			&e.ID, &e.GroupID, &e.PaidBy, &e.PaidByName, &e.Amount, &e.Description,
			&e.CategoryID, &catName, &e.SplitType, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Category = catName
		expenses = append(expenses, e)
	}

	// Load splits for each expense
	for i := range expenses {
		splitRows, err := s.DB.Query(`
			SELECT es.id, es.user_id, u.name, es.amount
			FROM expense_splits es JOIN users u ON es.user_id = u.id
			WHERE es.expense_id = ?
		`, expenses[i].ID)
		if err != nil {
			return nil, err
		}
		for splitRows.Next() {
			var sp ExpenseSplit
			if err := splitRows.Scan(&sp.ID, &sp.UserID, &sp.UserName, &sp.Amount); err != nil {
				splitRows.Close()
				return nil, err
			}
			expenses[i].Splits = append(expenses[i].Splits, sp)
		}
		splitRows.Close()
	}

	return expenses, rows.Err()
}

// --- Settlements -----------------------------------------------------------

func (s *Service) AddSettlement(callerID int64, in AddSettlementInput) (*Settlement, error) {
	if in.Amount <= 0 {
		return nil, fmt.Errorf("%w: amount must be positive", ErrInvalidInput)
	}
	if in.FromUser == in.ToUser {
		return nil, fmt.Errorf("%w: cannot settle with yourself", ErrInvalidInput)
	}

	// Verify caller is a group member
	isMember, err := s.isGroupMember(in.GroupID, callerID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrForbidden
	}

	// Verify both parties are members
	for _, uid := range []int64{in.FromUser, in.ToUser} {
		m, err := s.isGroupMember(in.GroupID, uid)
		if err != nil {
			return nil, err
		}
		if !m {
			return nil, fmt.Errorf("%w: user %d is not a group member", ErrInvalidInput, uid)
		}
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO settlements (group_id, from_user, to_user, amount, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, in.GroupID, in.FromUser, in.ToUser, in.Amount, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()

	return s.getSettlement(id)
}

func (s *Service) DeleteSettlement(callerID, settlementID int64) error {
	var groupID int64
	err := s.DB.QueryRow(`SELECT group_id FROM settlements WHERE id = ? AND deleted_at IS NULL`, settlementID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	isMember, err := s.isGroupMember(groupID, callerID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrForbidden
	}
	_, err = s.DB.Exec(`UPDATE settlements SET deleted_at = ? WHERE id = ?`, s.now(), settlementID)
	return err
}

func (s *Service) getSettlement(id int64) (*Settlement, error) {
	var st Settlement
	err := s.DB.QueryRow(`
		SELECT s.id, s.group_id, s.from_user, COALESCE(NULLIF(fu.name, ''), fu.email),
		       s.to_user, COALESCE(NULLIF(tu.name, ''), tu.email), s.amount, s.created_at, s.deleted_at
		FROM settlements s
		JOIN users fu ON s.from_user = fu.id
		JOIN users tu ON s.to_user = tu.id
		WHERE s.id = ?
	`, id).Scan(
		&st.ID, &st.GroupID, &st.FromUser, &st.FromUserName, &st.ToUser, &st.ToUserName,
		&st.Amount, &st.CreatedAt, &st.DeletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &st, err
}

// --- Balances --------------------------------------------------------------

// GetBalancesForUser returns the net balance between the caller and every
// other user they share groups with, with per-group breakdowns.
func (s *Service) GetBalancesForUser(userID int64) (int64, []PairBalance, error) {
	// Get all groups the user is in
	rows, err := s.DB.Query(
		`SELECT group_id FROM group_members WHERE user_id = ?`, userID,
	)
	if err != nil {
		return 0, nil, err
	}
	var groupIDs []int64
	for rows.Next() {
		var gid int64
		if err := rows.Scan(&gid); err != nil {
			rows.Close()
			return 0, nil, err
		}
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()

	if len(groupIDs) == 0 {
		return 0, nil, nil
	}

	// For each group, calculate pairwise balances between userID and every other member
	type key struct {
		userID  int64
		groupID int64
	}
	pairGroupNets := make(map[key]int64) // net per (otherUser, group)
	userNames := make(map[int64]string)
	groupNames := make(map[int64]string)

	for _, gid := range groupIDs {
		// Get group name
		var gname string
		s.DB.QueryRow(`SELECT name FROM groups WHERE id = ?`, gid).Scan(&gname)
		groupNames[gid] = gname

		// Get all active expenses in this group
		expRows, err := s.DB.Query(`
			SELECT e.id, e.paid_by, e.amount
			FROM expenses e
			WHERE e.group_id = ? AND e.deleted_at IS NULL
		`, gid)
		if err != nil {
			return 0, nil, err
		}

		type expInfo struct {
			id     int64
			paidBy int64
			amount int64
		}
		var exps []expInfo
		for expRows.Next() {
			var ei expInfo
			if err := expRows.Scan(&ei.id, &ei.paidBy, &ei.amount); err != nil {
				expRows.Close()
				return 0, nil, err
			}
			exps = append(exps, ei)
		}
		expRows.Close()

		for _, exp := range exps {
			// Get splits for this expense
			splitRows, err := s.DB.Query(
				`SELECT user_id, amount FROM expense_splits WHERE expense_id = ?`, exp.id,
			)
			if err != nil {
				return 0, nil, err
			}
			type split struct {
				userID int64
				amount int64
			}
			var splits []split
			for splitRows.Next() {
				var sp split
				if err := splitRows.Scan(&sp.userID, &sp.amount); err != nil {
					splitRows.Close()
					return 0, nil, err
				}
				splits = append(splits, sp)
			}
			splitRows.Close()

			if exp.paidBy == userID {
				// I paid: everyone who has a split owes me their share
				for _, sp := range splits {
					if sp.userID != userID {
						k := key{sp.userID, gid}
						pairGroupNets[k] += sp.amount // they owe me
					}
				}
			} else {
				// Someone else paid: I owe them my share
				for _, sp := range splits {
					if sp.userID == userID {
						k := key{exp.paidBy, gid}
						pairGroupNets[k] -= sp.amount // I owe them
					}
				}
			}
		}

		// Account for settlements in this group
		setRows, err := s.DB.Query(`
			SELECT from_user, to_user, amount
			FROM settlements
			WHERE group_id = ? AND deleted_at IS NULL
		`, gid)
		if err != nil {
			return 0, nil, err
		}
		for setRows.Next() {
			var fromU, toU, amt int64
			if err := setRows.Scan(&fromU, &toU, &amt); err != nil {
				setRows.Close()
				return 0, nil, err
			}
			if fromU == userID {
				// I paid someone → reduces what I owe them (or increases what they owe me)
				k := key{toU, gid}
				pairGroupNets[k] += amt
			} else if toU == userID {
				// Someone paid me → reduces what they owe me
				k := key{fromU, gid}
				pairGroupNets[k] -= amt
			}
		}
		setRows.Close()
	}

	// Collect user names
	for k := range pairGroupNets {
		if _, ok := userNames[k.userID]; !ok {
			var name string
			s.DB.QueryRow(`SELECT name FROM users WHERE id = ?`, k.userID).Scan(&name)
			if name == "" {
				var email string
				s.DB.QueryRow(`SELECT email FROM users WHERE id = ?`, k.userID).Scan(&email)
				name = email
			}
			userNames[k.userID] = name
		}
	}

	// Aggregate into PairBalances
	pairNets := make(map[int64]int64)
	pairGroups := make(map[int64][]GroupBalance)
	for k, net := range pairGroupNets {
		if net == 0 {
			continue
		}
		pairNets[k.userID] += net
		pairGroups[k.userID] = append(pairGroups[k.userID], GroupBalance{
			GroupID:   k.groupID,
			GroupName: groupNames[k.groupID],
			Net:       net,
		})
	}

	var totalNet int64
	var balances []PairBalance
	for uid, net := range pairNets {
		if net == 0 {
			continue
		}
		totalNet += net
		balances = append(balances, PairBalance{
			UserID:   uid,
			UserName: userNames[uid],
			Net:      net,
			Groups:   pairGroups[uid],
		})
	}

	// Sort by absolute net descending
	sort.Slice(balances, func(i, j int) bool {
		ai, aj := balances[i].Net, balances[j].Net
		if ai < 0 {
			ai = -ai
		}
		if aj < 0 {
			aj = -aj
		}
		return ai > aj
	})

	return totalNet, balances, nil
}

// GetGroupBalances returns net balances for all members within a group.
func (s *Service) GetGroupBalances(groupID int64) (map[int64]int64, error) {
	nets := make(map[int64]int64)

	// From expenses: payer gets +amount, each split member gets -splitAmount
	rows, err := s.DB.Query(`
		SELECT e.paid_by, es.user_id, es.amount
		FROM expenses e
		JOIN expense_splits es ON e.id = es.expense_id
		WHERE e.group_id = ? AND e.deleted_at IS NULL
	`, groupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var paidBy, splitUser, amt int64
		if err := rows.Scan(&paidBy, &splitUser, &amt); err != nil {
			rows.Close()
			return nil, err
		}
		if paidBy != splitUser {
			nets[paidBy] += amt
			nets[splitUser] -= amt
		}
	}
	rows.Close()

	// From settlements
	setRows, err := s.DB.Query(`
		SELECT from_user, to_user, amount FROM settlements
		WHERE group_id = ? AND deleted_at IS NULL
	`, groupID)
	if err != nil {
		return nil, err
	}
	for setRows.Next() {
		var fromU, toU, amt int64
		if err := setRows.Scan(&fromU, &toU, &amt); err != nil {
			setRows.Close()
			return nil, err
		}
		nets[fromU] += amt
		nets[toU] -= amt
	}
	setRows.Close()

	return nets, nil
}

// SuggestPayments returns the minimum set of payments to settle all debts
// within a group.
func (s *Service) SuggestPayments(groupID int64) ([]SuggestedPayment, error) {
	nets, err := s.GetGroupBalances(groupID)
	if err != nil {
		return nil, err
	}

	// Collect user names
	names := make(map[int64]string)
	for uid := range nets {
		var name string
		s.DB.QueryRow(`SELECT COALESCE(NULLIF(name, ''), email) FROM users WHERE id = ?`, uid).Scan(&name)
		names[uid] = name
	}

	type entry struct {
		userID int64
		amount int64
	}

	var debtors, creditors []entry
	for uid, net := range nets {
		if net < 0 {
			debtors = append(debtors, entry{uid, -net}) // positive amount they owe
		} else if net > 0 {
			creditors = append(creditors, entry{uid, net})
		}
	}

	// Sort both descending by amount for greedy matching
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].amount > debtors[j].amount })
	sort.Slice(creditors, func(i, j int) bool { return creditors[i].amount > creditors[j].amount })

	var payments []SuggestedPayment
	di, ci := 0, 0
	for di < len(debtors) && ci < len(creditors) {
		d := &debtors[di]
		c := &creditors[ci]
		amt := d.amount
		if c.amount < amt {
			amt = c.amount
		}
		payments = append(payments, SuggestedPayment{
			FromUser:     d.userID,
			FromUserName: names[d.userID],
			ToUser:       c.userID,
			ToUserName:   names[c.userID],
			Amount:       amt,
		})
		d.amount -= amt
		c.amount -= amt
		if d.amount == 0 {
			di++
		}
		if c.amount == 0 {
			ci++
		}
	}

	return payments, nil
}

// --- Timeline --------------------------------------------------------------

func (s *Service) GetTimeline(groupID int64, category, search string) ([]TimelineEntry, error) {
	var entries []TimelineEntry

	// Expenses
	expQuery := `
		SELECT e.id, e.amount, e.description, COALESCE(c.name, ''), COALESCE(NULLIF(u.name, ''), u.email), e.split_type, e.created_at
		FROM expenses e
		JOIN users u ON e.paid_by = u.id
		LEFT JOIN categories c ON e.category_id = c.id
		WHERE e.group_id = ? AND e.deleted_at IS NULL
	`
	args := []any{groupID}
	if category != "" {
		expQuery += ` AND c.name = ?`
		args = append(args, category)
	}
	if search != "" {
		expQuery += ` AND e.description LIKE ?`
		args = append(args, "%"+search+"%")
	}

	rows, err := s.DB.Query(expQuery, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var te TimelineEntry
		if err := rows.Scan(&te.ID, &te.Amount, &te.Description, &te.Category, &te.PaidByName, &te.SplitType, &te.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		te.Type = "expense"
		entries = append(entries, te)
	}
	rows.Close()

	// Settlements (only if no category filter)
	if category == "" {
		setQuery := `
			SELECT s.id, s.amount, COALESCE(NULLIF(fu.name, ''), fu.email), COALESCE(NULLIF(tu.name, ''), tu.email), s.created_at
			FROM settlements s
			JOIN users fu ON s.from_user = fu.id
			JOIN users tu ON s.to_user = tu.id
			WHERE s.group_id = ? AND s.deleted_at IS NULL
		`
		setArgs := []any{groupID}
		if search != "" {
			setQuery += ` AND (fu.name LIKE ? OR tu.name LIKE ?)`
			setArgs = append(setArgs, "%"+search+"%", "%"+search+"%")
		}

		setRows, err := s.DB.Query(setQuery, setArgs...)
		if err != nil {
			return nil, err
		}
		for setRows.Next() {
			var te TimelineEntry
			if err := setRows.Scan(&te.ID, &te.Amount, &te.FromName, &te.ToName, &te.CreatedAt); err != nil {
				setRows.Close()
				return nil, err
			}
			te.Type = "settlement"
			te.Description = fmt.Sprintf("%s paid %s", te.FromName, te.ToName)
			entries = append(entries, te)
		}
		setRows.Close()
	}

	// Sort by created_at descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt > entries[j].CreatedAt
	})

	return entries, nil
}

// --- Monthly Overview ------------------------------------------------------

func (s *Service) GetMonthlyOverview(userID int64) ([]MonthSummary, error) {
	// Get all groups the user is in
	groupIDs, err := s.userGroupIDs(userID)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return nil, nil
	}

	// Build placeholders
	placeholders := make([]string, len(groupIDs))
	args := make([]any, len(groupIDs))
	for i, gid := range groupIDs {
		placeholders[i] = "?"
		args[i] = gid
	}
	inClause := strings.Join(placeholders, ",")

	// Get expenses grouped by month and group
	rows, err := s.DB.Query(fmt.Sprintf(`
		SELECT
			strftime('%%Y-%%m', e.created_at, 'unixepoch') AS month,
			e.group_id, g.name, SUM(e.amount)
		FROM expenses e
		JOIN groups g ON e.group_id = g.id
		WHERE e.group_id IN (%s) AND e.deleted_at IS NULL
		GROUP BY month, e.group_id
		ORDER BY month DESC, g.name ASC
	`, inClause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	monthMap := make(map[string]*MonthSummary)
	var months []string
	for rows.Next() {
		var month, groupName string
		var groupID, total int64
		if err := rows.Scan(&month, &groupID, &groupName, &total); err != nil {
			return nil, err
		}
		ms, ok := monthMap[month]
		if !ok {
			ms = &MonthSummary{Month: month}
			monthMap[month] = ms
			months = append(months, month)
		}
		ms.Total += total
		ms.ByGroup = append(ms.ByGroup, GroupSpend{GroupID: groupID, GroupName: groupName, Total: total})
	}

	var result []MonthSummary
	for _, m := range months {
		result = append(result, *monthMap[m])
	}
	return result, nil
}

func (s *Service) GetMonthDetail(userID int64, month string) (*MonthSummary, error) {
	groupIDs, err := s.userGroupIDs(userID)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return nil, ErrNotFound
	}

	placeholders := make([]string, len(groupIDs))
	args := make([]any, len(groupIDs))
	for i, gid := range groupIDs {
		placeholders[i] = "?"
		args[i] = gid
	}
	inClause := strings.Join(placeholders, ",")
	args = append(args, month)

	// By group
	var byGroup []GroupSpend
	var total int64
	rows, err := s.DB.Query(fmt.Sprintf(`
		SELECT e.group_id, g.name, SUM(e.amount)
		FROM expenses e
		JOIN groups g ON e.group_id = g.id
		WHERE e.group_id IN (%s) AND e.deleted_at IS NULL
		  AND strftime('%%Y-%%m', e.created_at, 'unixepoch') = ?
		GROUP BY e.group_id
		ORDER BY SUM(e.amount) DESC
	`, inClause), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var gs GroupSpend
		if err := rows.Scan(&gs.GroupID, &gs.GroupName, &gs.Total); err != nil {
			rows.Close()
			return nil, err
		}
		total += gs.Total
		byGroup = append(byGroup, gs)
	}
	rows.Close()

	// By category
	catArgs := make([]any, len(groupIDs))
	copy(catArgs, args[:len(groupIDs)])
	catArgs = append(catArgs, month)

	var byCat []CategorySpend
	catRows, err := s.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(e.category_id, 0), COALESCE(c.name, 'uncategorized'), SUM(e.amount)
		FROM expenses e
		LEFT JOIN categories c ON e.category_id = c.id
		WHERE e.group_id IN (%s) AND e.deleted_at IS NULL
		  AND strftime('%%Y-%%m', e.created_at, 'unixepoch') = ?
		GROUP BY COALESCE(e.category_id, 0)
		ORDER BY SUM(e.amount) DESC
	`, inClause), catArgs...)
	if err != nil {
		return nil, err
	}
	for catRows.Next() {
		var cs CategorySpend
		if err := catRows.Scan(&cs.CategoryID, &cs.CategoryName, &cs.Total); err != nil {
			catRows.Close()
			return nil, err
		}
		byCat = append(byCat, cs)
	}
	catRows.Close()

	return &MonthSummary{
		Month:      month,
		Total:      total,
		ByGroup:    byGroup,
		ByCategory: byCat,
	}, nil
}

func (s *Service) userGroupIDs(userID int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT group_id FROM group_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
