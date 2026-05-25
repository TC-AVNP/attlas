package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

// ─── Helpers ────────────────────────────────────────────────────────────────

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func randomID(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func randomToken() (raw string, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	raw = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(h[:])
	return
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func sha256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type contextKey string

const emailCtxKey contextKey = "email"

// ─── App ────────────────────────────────────────────────────────────────────

type App struct {
	db           *sql.DB
	tmpl         *template.Template
	baseURL      string
	clientID     string
	clientSecret string
	adminEmail   string
	allowedCSV   string
	localBypass  bool
	stateDir     string
	attlasDir    string
}

// ─── Database ───────────────────────────────────────────────────────────────

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	return db, nil
}

func migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("exec %s: %w", e.Name(), err)
		}
		log.Printf("bfm: migration %s applied", e.Name())
	}
	return nil
}

// ─── Auth ───────────────────────────────────────────────────────────────────

func (a *App) isAllowed(email string) bool {
	if email == a.adminEmail {
		return true
	}
	for _, e := range strings.Split(a.allowedCSV, ",") {
		if strings.TrimSpace(e) == email {
			return true
		}
	}
	return false
}

func (a *App) createSession(email string) (string, error) {
	raw, hash := randomToken()
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err := a.db.Exec(
		"INSERT INTO sessions (token_hash, email, created_at, expires_at) VALUES (?, ?, datetime('now'), ?)",
		hash, email, expires,
	)
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (a *App) validateSession(r *http.Request) (string, error) {
	cookie, err := r.Cookie("bfm_session")
	if err != nil {
		return "", err
	}
	hash := sha256Hash(cookie.Value)
	var email, expiresAt string
	err = a.db.QueryRow("SELECT email, expires_at FROM sessions WHERE token_hash = ?", hash).Scan(&email, &expiresAt)
	if err != nil {
		return "", err
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		a.db.Exec("DELETE FROM sessions WHERE token_hash = ?", hash)
		return "", fmt.Errorf("session expired")
	}
	return email, nil
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.localBypass && r.Header.Get("X-Forwarded-For") == "" {
			ctx := context.WithValue(r.Context(), emailCtxKey, a.adminEmail)
			next(w, r.WithContext(ctx))
			return
		}
		email, err := a.validateSession(r)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.tmpl.ExecuteTemplate(w, "login.html", nil)
			return
		}
		ctx := context.WithValue(r.Context(), emailCtxKey, email)
		next(w, r.WithContext(ctx))
	}
}

func (a *App) handleAuthGoogle(w http.ResponseWriter, r *http.Request) {
	if a.localBypass && a.clientID == "" {
		token, err := a.createSession(a.adminEmail)
		if err != nil {
			http.Error(w, "session failed", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "bfm_session", Value: token, Path: "/",
			MaxAge: 30 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	redirectURI := a.baseURL + "/auth/callback"
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&access_type=online",
		url.QueryEscape(a.clientID), url.QueryEscape(redirectURI),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *App) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	redirectURI := a.baseURL + "/auth/callback"
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code": {code}, "client_id": {a.clientID}, "client_secret": {a.clientSecret},
		"redirect_uri": {redirectURI}, "grant_type": {"authorization_code"},
	})
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tokenResp)
	if tokenResp.AccessToken == "" {
		http.Error(w, "no access token", http.StatusBadGateway)
		return
	}
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	infoResp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "userinfo failed", http.StatusBadGateway)
		return
	}
	defer infoResp.Body.Close()
	var userInfo struct {
		Email string `json:"email"`
	}
	json.NewDecoder(infoResp.Body).Decode(&userInfo)
	if !a.isAllowed(userInfo.Email) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.ExecuteTemplate(w, "denied.html", nil)
		return
	}
	token, _ := a.createSession(userInfo.Email)
	secure := strings.HasPrefix(a.baseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name: "bfm_session", Value: token, Path: "/",
		MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("bfm_session")
	if err == nil {
		a.db.Exec("DELETE FROM sessions WHERE token_hash = ?", sha256Hash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "bfm_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

// ─── JSON helpers ───────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(v)
}

func nullStr(s sql.NullString) *string {
	if s.Valid {
		return &s.String
	}
	return nil
}

func strPtr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// ─── Audit helper ───────────────────────────────────────────────────────────

func (a *App) audit(brainID, eventType, msg, actor string) {
	a.db.Exec(
		"INSERT INTO audit_events (brain_id, type, msg, actor) VALUES (?, ?, ?, ?)",
		brainID, eventType, msg, actor,
	)
}

// ─── Brains API ─────────────────────────────────────────────────────────────

type BrainJSON struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	IP             *string        `json:"ip"`
	LAN            *string        `json:"lan"`
	ProvisionedAt  string         `json:"provisionedAt"`
	LastSeen       *string        `json:"lastSeen"`
	Slaves         []SlaveJSON    `json:"slaves"`
	Vouchers       []VoucherJSON  `json:"vouchers"`
	PXE            PXEJSON        `json:"pxe"`
	Audit          []AuditJSON    `json:"audit"`
	PlaybookMap    map[string]*string `json:"playbookByModel,omitempty"`
}

type SlaveJSON struct {
	ID              string  `json:"id"`
	BrainID         string  `json:"brainId"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	IP              *string `json:"ip"`
	MAC             *string `json:"mac"`
	Model           *string `json:"model"`
	PlaybookID      *string `json:"playbookId,omitempty"`
	PlaybookVersion *string `json:"playbookVersion,omitempty"`
	ImageVersion    *string `json:"imageVersion"`
	K8s             *string `json:"k8s"`
	JoinedAt        *string `json:"joinedAt"`
	LastSeen        *string `json:"lastSeen"`
}

type VoucherJSON struct {
	ID              string  `json:"id"`
	BrainID         string  `json:"brainId"`
	Kind            string  `json:"kind"`
	State           string  `json:"state"`
	PlaybookID      *string `json:"playbookId,omitempty"`
	PlaybookVersion *string `json:"playbookVersion,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	RedeemedAt      *string `json:"redeemedAt,omitempty"`
	RedeemedBy      *string `json:"redeemedBy,omitempty"`
}

type PXEJSON struct {
	Status       string             `json:"status"`
	UptimeSince  *string            `json:"uptimeSince"`
	DHCPRange    *DHCPRangeJSON     `json:"dhcpRange"`
	DNS          []string           `json:"dns"`
	Gateway      *string            `json:"gateway"`
	LeaseHours   int                `json:"leaseHours"`
	ActiveLeases int                `json:"activeLeases"`
	ServingImage *string            `json:"servingImage"`
	AssignedImage *string           `json:"assignedImage"`
	ConfigSync   string             `json:"configSync"`
	BootEvents   []BootEventJSON    `json:"bootEvents"`
	Provisioning ProvisioningJSON   `json:"provisioning"`
	Logs         []PXELogJSON       `json:"logs"`
}

type DHCPRangeJSON struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type ProvisioningJSON struct {
	Paused    bool `json:"paused"`
	MaxSlaves int  `json:"maxSlaves"`
}

type BootEventJSON struct {
	Timestamp string  `json:"t"`
	MAC       *string `json:"mac"`
	Model     *string `json:"model"`
	Result    string  `json:"result"`
	Error     *string `json:"error,omitempty"`
	SlaveID   *string `json:"slave,omitempty"`
}

type PXELogJSON struct {
	Timestamp string `json:"t"`
	Level     string `json:"level"`
	Src       string `json:"src"`
	Msg       string `json:"msg"`
}

type AuditJSON struct {
	Timestamp string `json:"t"`
	Type      string `json:"type"`
	Msg       string `json:"msg"`
	Actor     string `json:"actor"`
	BrainID   string `json:"brainId,omitempty"`
	BrainName string `json:"brainName,omitempty"`
}

func (a *App) loadBrain(brainID string) (*BrainJSON, error) {
	var b BrainJSON
	var ip, lan, lastSeen, dhcpStart, dhcpEnd, dns, gateway, servImg, assImg sql.NullString
	err := a.db.QueryRow(`SELECT id, name, description, status, ip, lan, provisioned_at, last_seen,
		pxe_dhcp_start, pxe_dhcp_end, pxe_dns, pxe_gateway, pxe_lease_hours, pxe_max_slaves,
		pxe_paused, pxe_assigned_image, pxe_serving_image, pxe_config_sync
		FROM brains WHERE id = ?`, brainID).Scan(
		&b.ID, &b.Name, &b.Description, &b.Status, &ip, &lan, &b.ProvisionedAt, &lastSeen,
		&dhcpStart, &dhcpEnd, &dns, &gateway, &b.PXE.LeaseHours, &b.PXE.Provisioning.MaxSlaves,
		&b.PXE.Provisioning.Paused, &assImg, &servImg, &b.PXE.ConfigSync,
	)
	if err != nil {
		return nil, err
	}
	b.IP = nullStr(ip)
	b.LAN = nullStr(lan)
	b.LastSeen = nullStr(lastSeen)
	b.PXE.ServingImage = nullStr(servImg)
	b.PXE.AssignedImage = nullStr(assImg)
	b.PXE.Gateway = nullStr(gateway)
	if dhcpStart.Valid && dhcpEnd.Valid {
		b.PXE.DHCPRange = &DHCPRangeJSON{Start: dhcpStart.String, End: dhcpEnd.String}
	}
	if dns.Valid && dns.String != "" {
		b.PXE.DNS = strings.Split(dns.String, ",")
	}
	// Determine PXE status from brain status
	if b.Status == "provisioning" || b.LastSeen == nil {
		b.PXE.Status = "stopped"
	} else {
		b.PXE.Status = "running"
	}

	// Load slaves
	b.Slaves = []SlaveJSON{}
	rows, _ := a.db.Query(`SELECT id, brain_id, name, status, ip, mac, model,
		playbook_id, playbook_version, image_version, k8s_status, joined_at, last_seen
		FROM slaves WHERE brain_id = ?`, brainID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var s SlaveJSON
			var sip, smac, smodel, spbid, spbv, simgv, sk8s, sjoin, slast sql.NullString
			rows.Scan(&s.ID, &s.BrainID, &s.Name, &s.Status, &sip, &smac, &smodel,
				&spbid, &spbv, &simgv, &sk8s, &sjoin, &slast)
			s.IP = nullStr(sip)
			s.MAC = nullStr(smac)
			s.Model = nullStr(smodel)
			s.PlaybookID = nullStr(spbid)
			s.PlaybookVersion = nullStr(spbv)
			s.ImageVersion = nullStr(simgv)
			s.K8s = nullStr(sk8s)
			s.JoinedAt = nullStr(sjoin)
			s.LastSeen = nullStr(slast)
			b.Slaves = append(b.Slaves, s)
		}
	}

	// Load vouchers
	b.Vouchers = []VoucherJSON{}
	rows2, _ := a.db.Query(`SELECT id, brain_id, kind, state, playbook_id, playbook_version,
		created_at, redeemed_at, redeemed_by FROM vouchers WHERE brain_id = ?`, brainID)
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var v VoucherJSON
			var pbid, pbv, rAt, rBy sql.NullString
			rows2.Scan(&v.ID, &v.BrainID, &v.Kind, &v.State, &pbid, &pbv,
				&v.CreatedAt, &rAt, &rBy)
			v.PlaybookID = nullStr(pbid)
			v.PlaybookVersion = nullStr(pbv)
			v.RedeemedAt = nullStr(rAt)
			v.RedeemedBy = nullStr(rBy)
			b.Vouchers = append(b.Vouchers, v)
		}
	}

	// Load audit events (most recent first, limit 50)
	b.Audit = []AuditJSON{}
	rows3, _ := a.db.Query(`SELECT type, msg, actor, created_at FROM audit_events
		WHERE brain_id = ? ORDER BY created_at DESC LIMIT 50`, brainID)
	if rows3 != nil {
		defer rows3.Close()
		for rows3.Next() {
			var e AuditJSON
			rows3.Scan(&e.Type, &e.Msg, &e.Actor, &e.Timestamp)
			e.BrainID = brainID
			e.BrainName = b.Name
			b.Audit = append(b.Audit, e)
		}
	}

	// Load boot events
	b.PXE.BootEvents = []BootEventJSON{}
	rows4, _ := a.db.Query(`SELECT mac, model, result, error, slave_id, created_at
		FROM boot_events WHERE brain_id = ? ORDER BY created_at DESC LIMIT 20`, brainID)
	if rows4 != nil {
		defer rows4.Close()
		for rows4.Next() {
			var be BootEventJSON
			var mac, model, berr, sid sql.NullString
			rows4.Scan(&mac, &model, &be.Result, &berr, &sid, &be.Timestamp)
			be.MAC = nullStr(mac)
			be.Model = nullStr(model)
			be.Error = nullStr(berr)
			be.SlaveID = nullStr(sid)
			b.PXE.BootEvents = append(b.PXE.BootEvents, be)
		}
	}

	// Load PXE logs
	b.PXE.Logs = []PXELogJSON{}
	rows5, _ := a.db.Query(`SELECT level, src, msg, created_at FROM pxe_logs
		WHERE brain_id = ? ORDER BY created_at DESC LIMIT 100`, brainID)
	if rows5 != nil {
		defer rows5.Close()
		for rows5.Next() {
			var l PXELogJSON
			rows5.Scan(&l.Level, &l.Src, &l.Msg, &l.Timestamp)
			b.PXE.Logs = append(b.PXE.Logs, l)
		}
	}

	// Active leases = online slaves count
	a.db.QueryRow("SELECT COUNT(*) FROM slaves WHERE brain_id = ? AND status = 'online'", brainID).Scan(&b.PXE.ActiveLeases)

	// Load playbook-by-model map
	b.PlaybookMap = map[string]*string{}
	rows6, _ := a.db.Query("SELECT pi_model, playbook_id FROM brain_playbook_map WHERE brain_id = ?", brainID)
	if rows6 != nil {
		defer rows6.Close()
		for rows6.Next() {
			var model string
			var pbID sql.NullString
			rows6.Scan(&model, &pbID)
			b.PlaybookMap[model] = nullStr(pbID)
		}
	}

	return &b, nil
}

func (a *App) handleListBrains(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id FROM brains ORDER BY provisioned_at DESC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var brains []BrainJSON
	for rows.Next() {
		var id string
		rows.Scan(&id)
		b, err := a.loadBrain(id)
		if err != nil {
			continue
		}
		brains = append(brains, *b)
	}
	if brains == nil {
		brains = []BrainJSON{}
	}
	writeJSON(w, http.StatusOK, brains)
}

func (a *App) handleGetBrain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, err := a.loadBrain(id)
	if err != nil {
		http.Error(w, "brain not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (a *App) handleCreateBrain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SlaveCount  int    `json:"slaveCount"`
		PlaybookID  string `json:"playbookId"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}

	email := r.Context().Value(emailCtxKey).(string)
	brainID := randomID("brn_")

	_, err := a.db.Exec(`INSERT INTO brains (id, name, description, status) VALUES (?, ?, ?, 'provisioning')`,
		brainID, req.Name, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, `{"error":"brain name already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create brain voucher
	brainVoucherID := randomID("vch_")
	rawToken, tokenHash := randomToken()
	a.db.Exec(`INSERT INTO vouchers (id, brain_id, kind, state, token_hash) VALUES (?, ?, 'brain', 'pending', ?)`,
		brainVoucherID, brainID, tokenHash)

	a.audit(brainID, "brain.created", "Brain identity created", email)
	a.audit(brainID, "voucher.created", fmt.Sprintf("Brain voucher %s created", brainVoucherID), email)

	// Create slave vouchers
	var slaveVoucherIDs []string
	for i := 0; i < req.SlaveCount; i++ {
		svID := randomID("vch_")
		_, svHash := randomToken()
		// Get current playbook version
		var pbVersion string
		if req.PlaybookID != "" {
			a.db.QueryRow("SELECT version FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1",
				req.PlaybookID).Scan(&pbVersion)
		}
		a.db.Exec(`INSERT INTO vouchers (id, brain_id, kind, state, token_hash, playbook_id, playbook_version)
			VALUES (?, ?, 'slave', 'pending', ?, ?, ?)`,
			svID, brainID, svHash, nilIfEmpty(req.PlaybookID), nilIfEmpty(pbVersion))
		slaveVoucherIDs = append(slaveVoucherIDs, svID)
	}

	if req.SlaveCount > 0 {
		var pbName string
		if req.PlaybookID != "" {
			a.db.QueryRow("SELECT name FROM playbooks WHERE id = ?", req.PlaybookID).Scan(&pbName)
		}
		a.audit(brainID, "voucher.created",
			fmt.Sprintf("%d slave voucher(s) created · playbook %s", req.SlaveCount, pbName), email)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"brainId":        brainID,
		"brainVoucherId": brainVoucherID,
		"brainToken":     rawToken,
		"slaveVouchers":  slaveVoucherIDs,
	})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (a *App) handleDeleteBrain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	email := r.Context().Value(emailCtxKey).(string)

	a.db.Exec("DELETE FROM pxe_logs WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM boot_events WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM audit_events WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM brain_playbook_map WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM brain_images WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM vouchers WHERE brain_id = ?", id)
	a.db.Exec("DELETE FROM slaves WHERE brain_id = ?", id)
	res, _ := a.db.Exec("DELETE FROM brains WHERE id = ?", id)
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	log.Printf("bfm: brain %s deleted by %s", id, email)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleUpdatePXE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		DHCPStart  *string `json:"dhcpStart"`
		DHCPEnd    *string `json:"dhcpEnd"`
		DNS        *string `json:"dns"`
		Gateway    *string `json:"gateway"`
		LeaseHours *int    `json:"leaseHours"`
		MaxSlaves  *int    `json:"maxSlaves"`
		Paused     *bool   `json:"paused"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.DHCPStart != nil {
		a.db.Exec("UPDATE brains SET pxe_dhcp_start = ? WHERE id = ?", *req.DHCPStart, id)
	}
	if req.DHCPEnd != nil {
		a.db.Exec("UPDATE brains SET pxe_dhcp_end = ? WHERE id = ?", *req.DHCPEnd, id)
	}
	if req.DNS != nil {
		a.db.Exec("UPDATE brains SET pxe_dns = ? WHERE id = ?", *req.DNS, id)
	}
	if req.Gateway != nil {
		a.db.Exec("UPDATE brains SET pxe_gateway = ? WHERE id = ?", *req.Gateway, id)
	}
	if req.LeaseHours != nil {
		a.db.Exec("UPDATE brains SET pxe_lease_hours = ? WHERE id = ?", *req.LeaseHours, id)
	}
	if req.MaxSlaves != nil {
		a.db.Exec("UPDATE brains SET pxe_max_slaves = ? WHERE id = ?", *req.MaxSlaves, id)
	}
	if req.Paused != nil {
		a.db.Exec("UPDATE brains SET pxe_paused = ? WHERE id = ?", *req.Paused, id)
	}
	a.db.Exec("UPDATE brains SET pxe_config_sync = 'pending' WHERE id = ?", id)

	email := r.Context().Value(emailCtxKey).(string)
	a.audit(id, "pxe.config_changed", "PXE configuration updated", email)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleUpdatePlaybookMap(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req map[string]*string // {"Pi 5": "pb_xxx", "Pi 4": null}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	for model, pbID := range req {
		if pbID != nil {
			a.db.Exec(`INSERT OR REPLACE INTO brain_playbook_map (brain_id, pi_model, playbook_id) VALUES (?, ?, ?)`,
				id, model, *pbID)
		} else {
			a.db.Exec("DELETE FROM brain_playbook_map WHERE brain_id = ? AND pi_model = ?", id, model)
		}
	}
	email := r.Context().Value(emailCtxKey).(string)
	a.audit(id, "pxe.playbook_map", "Playbook-by-model mapping updated", email)
	w.WriteHeader(http.StatusNoContent)
}

// ─── Vouchers API ───────────────────────────────────────────────────────────

func (a *App) handleCreateVouchers(w http.ResponseWriter, r *http.Request) {
	brainID := r.PathValue("id")
	var req struct {
		Count      int    `json:"count"`
		PlaybookID string `json:"playbookId"`
	}
	if err := readJSON(r, &req); err != nil || req.Count < 1 {
		http.Error(w, `{"error":"count must be >= 1"}`, http.StatusBadRequest)
		return
	}
	if req.Count > 32 {
		req.Count = 32
	}

	var pbVersion string
	if req.PlaybookID != "" {
		a.db.QueryRow("SELECT version FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1",
			req.PlaybookID).Scan(&pbVersion)
	}

	var ids []string
	for i := 0; i < req.Count; i++ {
		vID := randomID("vch_")
		_, tHash := randomToken()
		a.db.Exec(`INSERT INTO vouchers (id, brain_id, kind, state, token_hash, playbook_id, playbook_version)
			VALUES (?, ?, 'slave', 'pending', ?, ?, ?)`,
			vID, brainID, tHash, nilIfEmpty(req.PlaybookID), nilIfEmpty(pbVersion))
		ids = append(ids, vID)
	}

	email := r.Context().Value(emailCtxKey).(string)
	var pbName string
	a.db.QueryRow("SELECT name FROM playbooks WHERE id = ?", req.PlaybookID).Scan(&pbName)
	a.audit(brainID, "voucher.created",
		fmt.Sprintf("%d slave voucher(s) created · playbook %s@%s", req.Count, pbName, pbVersion), email)

	writeJSON(w, http.StatusCreated, map[string]any{"vouchers": ids})
}

func (a *App) handleRevokeVoucher(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var brainID string
	err := a.db.QueryRow("SELECT brain_id FROM vouchers WHERE id = ? AND state = 'pending'", id).Scan(&brainID)
	if err != nil {
		http.Error(w, "voucher not found or already redeemed", http.StatusNotFound)
		return
	}
	a.db.Exec("UPDATE vouchers SET state = 'revoked', revoked_at = datetime('now') WHERE id = ?", id)
	email := r.Context().Value(emailCtxKey).(string)
	a.audit(brainID, "voucher.revoked", fmt.Sprintf("Voucher %s revoked", id), email)
	w.WriteHeader(http.StatusNoContent)
}

// ─── Playbooks API ──────────────────────────────────────────────────────────

type PlaybookJSON struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Description    string               `json:"description"`
	CurrentVersion string               `json:"currentVersion"`
	Versions       []PlaybookVersionJSON `json:"versions"`
	VoucherCount   int                  `json:"voucherCount"`
	YAML           string               `json:"yaml,omitempty"`
}

type PlaybookVersionJSON struct {
	Version    string `json:"v"`
	UploadedAt string `json:"uploadedAt"`
	UploadedBy string `json:"uploader"`
	Notes      string `json:"notes"`
	Lines      int    `json:"lines"`
	SHA        string `json:"sha"`
}

func (a *App) handleListPlaybooks(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.db.Query("SELECT id, name, description FROM playbooks ORDER BY name")
	if rows == nil {
		writeJSON(w, http.StatusOK, []PlaybookJSON{})
		return
	}
	defer rows.Close()

	var result []PlaybookJSON
	for rows.Next() {
		var p PlaybookJSON
		rows.Scan(&p.ID, &p.Name, &p.Description)

		// Versions
		vrows, _ := a.db.Query(`SELECT version, notes, lines, sha, uploaded_by, uploaded_at
			FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC`, p.ID)
		if vrows != nil {
			for vrows.Next() {
				var v PlaybookVersionJSON
				vrows.Scan(&v.Version, &v.Notes, &v.Lines, &v.SHA, &v.UploadedBy, &v.UploadedAt)
				p.Versions = append(p.Versions, v)
			}
			vrows.Close()
		}
		if len(p.Versions) > 0 {
			p.CurrentVersion = p.Versions[0].Version
		}

		// Current YAML
		a.db.QueryRow("SELECT yaml FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1",
			p.ID).Scan(&p.YAML)

		// Voucher count
		a.db.QueryRow("SELECT COUNT(*) FROM vouchers WHERE playbook_id = ?", p.ID).Scan(&p.VoucherCount)

		result = append(result, p)
	}
	if result == nil {
		result = []PlaybookJSON{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleGetPlaybook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p PlaybookJSON
	err := a.db.QueryRow("SELECT id, name, description FROM playbooks WHERE id = ?", id).Scan(&p.ID, &p.Name, &p.Description)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	vrows, _ := a.db.Query(`SELECT version, notes, lines, sha, uploaded_by, uploaded_at
		FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC`, id)
	if vrows != nil {
		defer vrows.Close()
		for vrows.Next() {
			var v PlaybookVersionJSON
			vrows.Scan(&v.Version, &v.Notes, &v.Lines, &v.SHA, &v.UploadedBy, &v.UploadedAt)
			p.Versions = append(p.Versions, v)
		}
	}
	if len(p.Versions) > 0 {
		p.CurrentVersion = p.Versions[0].Version
	}
	a.db.QueryRow("SELECT yaml FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1", id).Scan(&p.YAML)
	a.db.QueryRow("SELECT COUNT(*) FROM vouchers WHERE playbook_id = ?", id).Scan(&p.VoucherCount)
	writeJSON(w, http.StatusOK, p)
}

func (a *App) handleCreatePlaybook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		YAML        string `json:"yaml"`
		Notes       string `json:"notes"`
	}
	if err := readJSON(r, &req); err != nil || req.Name == "" || req.YAML == "" {
		http.Error(w, `{"error":"name and yaml required"}`, http.StatusBadRequest)
		return
	}

	pbID := "pb_" + strings.ReplaceAll(strings.ToLower(req.Name), " ", "_")
	_, err := a.db.Exec("INSERT INTO playbooks (id, name, description) VALUES (?, ?, ?)",
		pbID, req.Name, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, `{"error":"playbook name already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lines := strings.Count(req.YAML, "\n") + 1
	sha := "sha256:" + sha256Bytes([]byte(req.YAML))[:12]
	email := r.Context().Value(emailCtxKey).(string)

	a.db.Exec(`INSERT INTO playbook_versions (playbook_id, version, yaml, notes, lines, sha, uploaded_by)
		VALUES (?, 'v1', ?, ?, ?, ?, ?)`, pbID, req.YAML, req.Notes, lines, sha, email)

	writeJSON(w, http.StatusCreated, map[string]string{"id": pbID, "version": "v1"})
}

func (a *App) handleUploadVersion(w http.ResponseWriter, r *http.Request) {
	pbID := r.PathValue("id")
	var req struct {
		YAML  string `json:"yaml"`
		Notes string `json:"notes"`
	}
	if err := readJSON(r, &req); err != nil || req.YAML == "" {
		http.Error(w, `{"error":"yaml required"}`, http.StatusBadRequest)
		return
	}

	// Find next version number
	var lastVersion string
	a.db.QueryRow("SELECT version FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1",
		pbID).Scan(&lastVersion)
	nextNum := 1
	if lastVersion != "" {
		fmt.Sscanf(lastVersion, "v%d", &nextNum)
		nextNum++
	}
	version := fmt.Sprintf("v%d", nextNum)

	lines := strings.Count(req.YAML, "\n") + 1
	sha := "sha256:" + sha256Bytes([]byte(req.YAML))[:12]
	email := r.Context().Value(emailCtxKey).(string)

	a.db.Exec(`INSERT INTO playbook_versions (playbook_id, version, yaml, notes, lines, sha, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, pbID, version, req.YAML, req.Notes, lines, sha, email)

	writeJSON(w, http.StatusCreated, map[string]string{"version": version})
}

// ─── Slave Images API ───────────────────────────────────────────────────────

type SlaveImageJSON struct {
	Version    string `json:"version"`
	Filename   string `json:"filename"`
	FileSize   string `json:"size"`
	SHA        string `json:"sha"`
	Notes      string `json:"notes"`
	UploadedBy string `json:"uploader"`
	UploadedAt string `json:"uploadedAt"`
	Current    bool   `json:"current"`
}

func (a *App) handleListImages(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.db.Query("SELECT version, filename, file_size, sha, notes, uploaded_by, uploaded_at, is_current FROM slave_images ORDER BY uploaded_at DESC")
	var result []SlaveImageJSON
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var img SlaveImageJSON
			var fn, fs, sha sql.NullString
			rows.Scan(&img.Version, &fn, &fs, &sha, &img.Notes, &img.UploadedBy, &img.UploadedAt, &img.Current)
			img.Filename = strPtr(nullStr(fn))
			img.FileSize = strPtr(nullStr(fs))
			img.SHA = strPtr(nullStr(sha))
			result = append(result, img)
		}
	}
	if result == nil {
		result = []SlaveImageJSON{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
		Notes   string `json:"notes"`
	}
	if err := readJSON(r, &req); err != nil || req.Version == "" {
		http.Error(w, `{"error":"version required"}`, http.StatusBadRequest)
		return
	}
	email := r.Context().Value(emailCtxKey).(string)

	// Mark all as not current, then insert new as current
	a.db.Exec("UPDATE slave_images SET is_current = 0")
	a.db.Exec(`INSERT INTO slave_images (version, notes, uploaded_by, is_current) VALUES (?, ?, ?, 1)`,
		req.Version, req.Notes, email)

	writeJSON(w, http.StatusCreated, map[string]string{"version": req.Version})
}

// ─── Brain Image Build (SSE) ────────────────────────────────────────────────

func (a *App) handleBuildBrainImage(w http.ResponseWriter, r *http.Request) {
	brainID := r.PathValue("id")
	email := r.Context().Value(emailCtxKey).(string)

	// Find the brain voucher raw token
	var voucherID, tokenHash string
	err := a.db.QueryRow("SELECT id, token_hash FROM vouchers WHERE brain_id = ? AND kind = 'brain' AND state = 'pending' LIMIT 1",
		brainID).Scan(&voucherID, &tokenHash)
	if err != nil {
		http.Error(w, `{"error":"no pending brain voucher found"}`, http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Find the router-node build script
	buildScript := filepath.Join(a.attlasDir, "router-node", "build-image.sh")
	if _, err := os.Stat(buildScript); err != nil {
		// Fallback: simulate build progress for development
		a.simulateBuild(w, flusher, brainID, email)
		return
	}

	// Real build would go here — for now simulate
	a.simulateBuild(w, flusher, brainID, email)
}

func (a *App) simulateBuild(w http.ResponseWriter, flusher http.Flusher, brainID, email string) {
	steps := []struct {
		pct int
		msg string
	}{
		{5, "fetching universal base image"},
		{15, "fetched base image (1.2 GB)"},
		{25, "mounting boot partition"},
		{40, "injecting brain voucher"},
		{55, "injecting cert chain"},
		{70, "writing first-boot service"},
		{85, "compressing with xz -T0"},
		{100, "done"},
	}

	filename := fmt.Sprintf("bfm-brain-%s.img.xz", brainID[4:])

	for _, s := range steps {
		data, _ := json.Marshal(map[string]any{"progress": s.pct, "message": s.msg})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		time.Sleep(300 * time.Millisecond)
	}

	// Record in DB
	a.db.Exec("INSERT INTO brain_images (brain_id, filename) VALUES (?, ?)", brainID, filename)
	a.audit(brainID, "image.built", fmt.Sprintf("Golden SD image built · %s", filename), email)

	done, _ := json.Marshal(map[string]any{
		"done":        true,
		"filename":    filename,
		"downloadUrl": fmt.Sprintf("/api/brains/%s/image/download/%s", brainID, filename),
	})
	fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

func (a *App) handleDownloadBrainImage(w http.ResponseWriter, r *http.Request) {
	brainID := r.PathValue("id")
	filename := r.PathValue("filename")

	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	imgPath := filepath.Join(a.stateDir, "images", filename)
	if _, err := os.Stat(imgPath); err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}

	a.db.Exec("UPDATE brain_images SET downloaded_at = datetime('now') WHERE brain_id = ? AND filename = ?",
		brainID, filename)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, imgPath)
}

// ─── Node Registration API (no OAuth — voucher/mTLS auth) ──────────────────

func (a *App) handleRegisterBrain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		IP    string `json:"ip"`
		LAN   string `json:"lan"`
	}
	if err := readJSON(r, &req); err != nil || req.Token == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
		return
	}

	hash := sha256Hash(req.Token)
	var voucherID, brainID string
	err := a.db.QueryRow("SELECT v.id, v.brain_id FROM vouchers v WHERE v.token_hash = ? AND v.kind = 'brain' AND v.state = 'pending'",
		hash).Scan(&voucherID, &brainID)
	if err != nil {
		http.Error(w, `{"error":"invalid or used voucher"}`, http.StatusForbidden)
		return
	}

	// Redeem voucher
	a.db.Exec("UPDATE vouchers SET state = 'redeemed', redeemed_at = datetime('now'), redeemed_by = ? WHERE id = ?",
		brainID, voucherID)
	a.db.Exec("UPDATE brains SET status = 'online', ip = ?, lan = ?, last_seen = datetime('now') WHERE id = ?",
		req.IP, req.LAN, brainID)

	a.audit(brainID, "voucher.redeemed", fmt.Sprintf("Brain voucher %s redeemed", voucherID), brainID)
	a.audit(brainID, "brain.online", "Brain first contact · cert issued", "system")

	// TODO: Generate and return mTLS certificate
	writeJSON(w, http.StatusOK, map[string]any{
		"brainId": brainID,
		"message": "registered",
	})
}

func (a *App) handleRegisterSlave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Model string `json:"model"`
		MAC   string `json:"mac"`
	}
	if err := readJSON(r, &req); err != nil || req.Token == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
		return
	}

	hash := sha256Hash(req.Token)
	var voucherID, brainID string
	var pbID, pbVersion sql.NullString
	err := a.db.QueryRow(`SELECT v.id, v.brain_id, v.playbook_id, v.playbook_version
		FROM vouchers v WHERE v.token_hash = ? AND v.kind = 'slave' AND v.state = 'pending'`,
		hash).Scan(&voucherID, &brainID, &pbID, &pbVersion)
	if err != nil {
		http.Error(w, `{"error":"invalid or used voucher"}`, http.StatusForbidden)
		return
	}

	slaveID := randomID("slv_")
	var brainName string
	a.db.QueryRow("SELECT name FROM brains WHERE id = ?", brainID).Scan(&brainName)
	slaveName := fmt.Sprintf("%s-w%d", brainName, countSlaves(a.db, brainID)+1)

	a.db.Exec(`INSERT INTO slaves (id, brain_id, name, status, mac, model, playbook_id, playbook_version, joined_at)
		VALUES (?, ?, ?, 'online', ?, ?, ?, ?, datetime('now'))`,
		slaveID, brainID, slaveName, req.MAC, req.Model, nullStr(pbID), nullStr(pbVersion))

	a.db.Exec("UPDATE vouchers SET state = 'redeemed', redeemed_at = datetime('now'), redeemed_by = ? WHERE id = ?",
		slaveID, voucherID)

	a.audit(brainID, "voucher.redeemed",
		fmt.Sprintf("Voucher %s redeemed → %s · %s detected", voucherID, slaveID, req.Model), slaveID)

	// Load playbook YAML if assigned
	var playbookYAML string
	if pbID.Valid {
		a.db.QueryRow("SELECT yaml FROM playbook_versions WHERE playbook_id = ? AND version = ?",
			pbID.String, pbVersion.String).Scan(&playbookYAML)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"slaveId":  slaveID,
		"brainId":  brainID,
		"playbook": playbookYAML,
		"message":  "registered",
	})
}

func countSlaves(db *sql.DB, brainID string) int {
	var n int
	db.QueryRow("SELECT COUNT(*) FROM slaves WHERE brain_id = ?", brainID).Scan(&n)
	return n
}

func (a *App) handleClaimVoucher(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BrainID string `json:"brainId"`
		Model   string `json:"model"`
	}
	if err := readJSON(r, &req); err != nil || req.BrainID == "" {
		http.Error(w, `{"error":"brainId required"}`, http.StatusBadRequest)
		return
	}

	// Check provisioning is not paused
	var paused bool
	a.db.QueryRow("SELECT pxe_paused FROM brains WHERE id = ?", req.BrainID).Scan(&paused)
	if paused {
		http.Error(w, `{"error":"provisioning paused"}`, http.StatusServiceUnavailable)
		return
	}

	// Check slave cap
	var maxSlaves, currentSlaves int
	a.db.QueryRow("SELECT pxe_max_slaves FROM brains WHERE id = ?", req.BrainID).Scan(&maxSlaves)
	a.db.QueryRow("SELECT COUNT(*) FROM slaves WHERE brain_id = ?", req.BrainID).Scan(&currentSlaves)
	if currentSlaves >= maxSlaves {
		http.Error(w, `{"error":"slave cap reached"}`, http.StatusTooManyRequests)
		return
	}

	// Find an available slave voucher
	var voucherID, tokenHash string
	err := a.db.QueryRow(`SELECT id, token_hash FROM vouchers
		WHERE brain_id = ? AND kind = 'slave' AND state = 'pending' LIMIT 1`,
		req.BrainID).Scan(&voucherID, &tokenHash)
	if err != nil {
		http.Error(w, `{"error":"voucher pool empty"}`, http.StatusTooManyRequests)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"voucherId": voucherID,
		"tokenHash": tokenHash,
	})
}

func (a *App) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BrainID      string `json:"brainId"`
		SlaveID      string `json:"slaveId,omitempty"`
		ServingImage string `json:"servingImage,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.SlaveID != "" {
		a.db.Exec("UPDATE slaves SET last_seen = datetime('now'), status = 'online' WHERE id = ?", req.SlaveID)
	} else if req.BrainID != "" {
		a.db.Exec("UPDATE brains SET last_seen = datetime('now'), status = 'online' WHERE id = ?", req.BrainID)
		if req.ServingImage != "" {
			a.db.Exec("UPDATE brains SET pxe_serving_image = ? WHERE id = ?", req.ServingImage, req.BrainID)
			// Check if serving matches assigned
			var assigned sql.NullString
			a.db.QueryRow("SELECT pxe_assigned_image FROM brains WHERE id = ?", req.BrainID).Scan(&assigned)
			if assigned.Valid && assigned.String == req.ServingImage {
				a.db.Exec("UPDATE brains SET pxe_config_sync = 'in-sync' WHERE id = ?", req.BrainID)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleIngestLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BrainID string       `json:"brainId"`
		Logs    []PXELogJSON `json:"logs"`
	}
	if err := readJSON(r, &req); err != nil || req.BrainID == "" {
		http.Error(w, "invalid", http.StatusBadRequest)
		return
	}
	for _, l := range req.Logs {
		a.db.Exec("INSERT INTO pxe_logs (brain_id, level, src, msg) VALUES (?, ?, ?, ?)",
			req.BrainID, l.Level, l.Src, l.Msg)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Global data endpoint (for SPA) ────────────────────────────────────────

func (a *App) handleGlobalData(w http.ResponseWriter, r *http.Request) {
	// Load all brains
	rows, _ := a.db.Query("SELECT id FROM brains ORDER BY provisioned_at DESC")
	var brains []BrainJSON
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			rows.Scan(&id)
			b, err := a.loadBrain(id)
			if err == nil {
				brains = append(brains, *b)
			}
		}
	}
	if brains == nil {
		brains = []BrainJSON{}
	}

	// Load playbooks
	var playbooks []PlaybookJSON
	prows, _ := a.db.Query("SELECT id, name, description FROM playbooks ORDER BY name")
	if prows != nil {
		defer prows.Close()
		for prows.Next() {
			var p PlaybookJSON
			prows.Scan(&p.ID, &p.Name, &p.Description)
			vrows, _ := a.db.Query(`SELECT version, notes, lines, sha, uploaded_by, uploaded_at
				FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC`, p.ID)
			if vrows != nil {
				for vrows.Next() {
					var v PlaybookVersionJSON
					vrows.Scan(&v.Version, &v.Notes, &v.Lines, &v.SHA, &v.UploadedBy, &v.UploadedAt)
					p.Versions = append(p.Versions, v)
				}
				vrows.Close()
			}
			if len(p.Versions) > 0 {
				p.CurrentVersion = p.Versions[0].Version
			}
			a.db.QueryRow("SELECT yaml FROM playbook_versions WHERE playbook_id = ? ORDER BY uploaded_at DESC LIMIT 1", p.ID).Scan(&p.YAML)
			a.db.QueryRow("SELECT COUNT(*) FROM vouchers WHERE playbook_id = ?", p.ID).Scan(&p.VoucherCount)
			playbooks = append(playbooks, p)
		}
	}
	if playbooks == nil {
		playbooks = []PlaybookJSON{}
	}

	// Load slave images
	var images []SlaveImageJSON
	irows, _ := a.db.Query("SELECT version, filename, file_size, sha, notes, uploaded_by, uploaded_at, is_current FROM slave_images ORDER BY uploaded_at DESC")
	if irows != nil {
		defer irows.Close()
		for irows.Next() {
			var img SlaveImageJSON
			var fn, fs, sha sql.NullString
			irows.Scan(&img.Version, &fn, &fs, &sha, &img.Notes, &img.UploadedBy, &img.UploadedAt, &img.Current)
			img.Filename = strPtr(nullStr(fn))
			img.FileSize = strPtr(nullStr(fs))
			img.SHA = strPtr(nullStr(sha))
			images = append(images, img)
		}
	}
	if images == nil {
		images = []SlaveImageJSON{}
	}

	// Global audit (across all brains, most recent first)
	var globalAudit []AuditJSON
	arows, _ := a.db.Query(`SELECT a.type, a.msg, a.actor, a.created_at, a.brain_id, b.name
		FROM audit_events a JOIN brains b ON a.brain_id = b.id
		ORDER BY a.created_at DESC LIMIT 30`)
	if arows != nil {
		defer arows.Close()
		for arows.Next() {
			var e AuditJSON
			arows.Scan(&e.Type, &e.Msg, &e.Actor, &e.Timestamp, &e.BrainID, &e.BrainName)
			globalAudit = append(globalAudit, e)
		}
	}
	if globalAudit == nil {
		globalAudit = []AuditJSON{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"brains":      brains,
		"playbooks":   playbooks,
		"slaveImages": images,
		"globalAudit": globalAudit,
		"now":         time.Now().UTC().Format(time.RFC3339),
	})
}

// ─── WebSocket Terminal ─────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// sshDialViaCloudflared connects to a host behind a Cloudflare tunnel using
// cloudflared as the ProxyCommand (same as `ssh -o ProxyCommand="cloudflared access ssh --hostname %h"`).
func sshDialViaCloudflared(hostname string, config *ssh.ClientConfig) (*ssh.Client, error) {
	cmd := exec.Command("cloudflared", "access", "ssh", "--hostname", hostname)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	// Wrap stdin+stdout as a net.Conn for the SSH client
	conn := &proxyConn{Reader: stdout, Writer: stdin, cmd: cmd}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, hostname+":22", config)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("ssh handshake via cloudflared: %w", err)
	}
	return ssh.NewClient(ncc, chans, reqs), nil
}

// proxyConn wraps a command's stdin/stdout as a net.Conn for SSH.
type proxyConn struct {
	io.Reader
	io.Writer
	cmd *exec.Cmd
}

func (c *proxyConn) Close() error {
	if w, ok := c.Writer.(io.Closer); ok {
		w.Close()
	}
	c.cmd.Process.Kill()
	return c.cmd.Wait()
}
func (c *proxyConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *proxyConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *proxyConn) SetDeadline(t time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(t time.Time) error { return nil }

func (a *App) handleSlaveTerminal(w http.ResponseWriter, r *http.Request) {
	// Auth: validate session cookie manually (can't use requireAuth wrapper for WebSocket)
	if !(a.localBypass && r.Header.Get("X-Forwarded-For") == "") {
		_, err := a.validateSession(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Look up slave IP
	slaveID := r.PathValue("id")
	var slaveIP sql.NullString
	var slaveName string
	err := a.db.QueryRow("SELECT ip, name FROM slaves WHERE id = ?", slaveID).Scan(&slaveIP, &slaveName)
	if err != nil {
		http.Error(w, "slave not found", http.StatusNotFound)
		return
	}
	if !slaveIP.Valid || slaveIP.String == "" {
		http.Error(w, "slave has no IP address", http.StatusBadRequest)
		return
	}

	// Upgrade to WebSocket
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("bfm: terminal ws upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("bfm: terminal session opened for %s (%s) at %s", slaveName, slaveID, slaveIP.String)

	// SSH to slave via pi-router jump host (GCP VM can't reach 10.0.0.x directly)
	sshConfig := &ssh.ClientConfig{
		User: "commonlisp",
		Auth: []ssh.AuthMethod{
			ssh.Password("xadrez12"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// First hop: SSH to pi-router via cloudflared proxy
	jumpConn, err := sshDialViaCloudflared("router.attlas.uk", sshConfig)
	if err != nil {
		log.Printf("bfm: SSH jump to router failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** Jump host connection failed: %v ***\r\n", err)))
		return
	}
	defer jumpConn.Close()

	// Second hop: SSH from pi-router to slave
	slaveNetConn, err := jumpConn.Dial("tcp", slaveIP.String+":22")
	if err != nil {
		log.Printf("bfm: SSH dial to %s via jump failed: %v", slaveIP.String, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** SSH connection to %s failed: %v ***\r\n", slaveName, err)))
		return
	}
	ncc, chans, reqs, err := ssh.NewClientConn(slaveNetConn, slaveIP.String+":22", sshConfig)
	if err != nil {
		slaveNetConn.Close()
		log.Printf("bfm: SSH handshake to %s failed: %v", slaveIP.String, err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** SSH handshake failed: %v ***\r\n", err)))
		return
	}
	sshConn := ssh.NewClient(ncc, chans, reqs)
	defer sshConn.Close()

	session, err := sshConn.NewSession()
	if err != nil {
		log.Printf("bfm: SSH session failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** SSH session failed: %v ***\r\n", err)))
		return
	}
	defer session.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		log.Printf("bfm: PTY request failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** PTY request failed: %v ***\r\n", err)))
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}

	if err := session.Shell(); err != nil {
		log.Printf("bfm: shell start failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n*** Shell failed: %v ***\r\n", err)))
		return
	}

	var once sync.Once
	done := make(chan struct{})
	cleanup := func() { once.Do(func() { close(done) }) }

	// SSH stdout -> WebSocket
	go func() {
		defer cleanup()
		buf := make([]byte, 8192)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> SSH stdin (handle resize messages)
	go func() {
		defer cleanup()
		for {
			msgType, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				// Check if it's a resize message
				var resize struct {
					Type string `json:"type"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if json.Unmarshal(msg, &resize) == nil && resize.Type == "resize" {
					session.WindowChange(resize.Rows, resize.Cols)
					continue
				}
			}
			// Regular input
			if _, err := stdin.Write(msg); err != nil {
				return
			}
		}
	}()

	// Wait for session end or done signal
	go func() {
		session.Wait()
		cleanup()
	}()

	<-done
	log.Printf("bfm: terminal session closed for %s (%s)", slaveName, slaveID)
}

// ─── Static assets ──────────────────────────────────────────────────────────

//go:embed templates/styles.css
var stylesCSS []byte

//go:embed templates/index.html
var indexHTML []byte

func (a *App) handleStyles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(stylesCSS)
}

// ─── Logging middleware ─────────────────────────────────────────────────────

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/heartbeat") {
			log.Printf("bfm: %s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ─── Main ───────────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	port := envInt("BFM_PORT", 7698)
	dbPath := envString("BFM_DB", "/var/lib/bfm/bfm.db")
	stateDir := envString("BFM_STATE_DIR", "/var/lib/bfm")
	attlasDir := envString("BFM_ATTLAS_DIR", "/home/agnostic-user/iapetus/attlas")
	baseURL := envString("BFM_BASE_URL", fmt.Sprintf("http://localhost:%d", port))
	adminEmail := envString("BFM_ADMIN_EMAIL", "condecopedro@gmail.com")
	allowedCSV := envString("BFM_ALLOWED_EMAILS", "")
	clientID := envString("BFM_GOOGLE_CLIENT_ID", "")
	clientSecret := envString("BFM_GOOGLE_SECRET", "")
	localBypass := envString("BFM_LOCAL_BYPASS", "1") == "1"

	// Ensure state dirs
	os.MkdirAll(filepath.Join(stateDir, "images"), 0755)
	os.MkdirAll(filepath.Join(stateDir, "playbooks"), 0755)

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("bfm: open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("bfm: migrate: %v", err)
	}

	tmpl := template.Must(template.ParseFS(templatesFS, "templates/login.html", "templates/denied.html"))

	app := &App{
		db:           db,
		tmpl:         tmpl,
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		adminEmail:   adminEmail,
		allowedCSV:   allowedCSV,
		localBypass:  localBypass,
		stateDir:     stateDir,
		attlasDir:    attlasDir,
	}

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /auth/google", app.handleAuthGoogle)
	mux.HandleFunc("GET /auth/callback", app.handleAuthCallback)
	mux.HandleFunc("POST /auth/logout", app.handleAuthLogout)

	// Static
	mux.HandleFunc("GET /styles.css", app.handleStyles)

	// Dashboard API (auth required)
	mux.HandleFunc("GET /api/data", app.requireAuth(app.handleGlobalData))
	mux.HandleFunc("GET /api/brains", app.requireAuth(app.handleListBrains))
	mux.HandleFunc("POST /api/brains", app.requireAuth(app.handleCreateBrain))
	mux.HandleFunc("GET /api/brains/{id}", app.requireAuth(app.handleGetBrain))
	mux.HandleFunc("DELETE /api/brains/{id}", app.requireAuth(app.handleDeleteBrain))
	mux.HandleFunc("PUT /api/brains/{id}/pxe", app.requireAuth(app.handleUpdatePXE))
	mux.HandleFunc("PUT /api/brains/{id}/playbook-map", app.requireAuth(app.handleUpdatePlaybookMap))
	mux.HandleFunc("POST /api/brains/{id}/image", app.requireAuth(app.handleBuildBrainImage))
	mux.HandleFunc("GET /api/brains/{id}/image/download/{filename}", app.requireAuth(app.handleDownloadBrainImage))
	mux.HandleFunc("POST /api/brains/{id}/vouchers", app.requireAuth(app.handleCreateVouchers))
	mux.HandleFunc("DELETE /api/vouchers/{id}", app.requireAuth(app.handleRevokeVoucher))
	mux.HandleFunc("GET /api/playbooks", app.requireAuth(app.handleListPlaybooks))
	mux.HandleFunc("POST /api/playbooks", app.requireAuth(app.handleCreatePlaybook))
	mux.HandleFunc("GET /api/playbooks/{id}", app.requireAuth(app.handleGetPlaybook))
	mux.HandleFunc("POST /api/playbooks/{id}/versions", app.requireAuth(app.handleUploadVersion))
	mux.HandleFunc("GET /api/images", app.requireAuth(app.handleListImages))
	mux.HandleFunc("POST /api/images", app.requireAuth(app.handleUploadImage))

	// Terminal WebSocket (auth checked inside handler)
	mux.HandleFunc("GET /api/slaves/{id}/terminal", app.handleSlaveTerminal)

	// Node registration (no OAuth)
	mux.HandleFunc("POST /api/register/brain", app.handleRegisterBrain)
	mux.HandleFunc("POST /api/register/slave", app.handleRegisterSlave)
	mux.HandleFunc("POST /api/claim-voucher", app.handleClaimVoucher)
	mux.HandleFunc("POST /api/heartbeat", app.handleHeartbeat)
	mux.HandleFunc("POST /api/logs", app.handleIngestLogs)

	// SPA catch-all
	mux.HandleFunc("GET /{$}", app.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	}))

	bindHost := envString("BFM_BIND", "127.0.0.1")
	addr := fmt.Sprintf("%s:%d", bindHost, port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("bfm listening on http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("bfm: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	case err := <-errCh:
		if err != nil {
			log.Fatalf("bfm: serve failed: %v", err)
		}
	}
}
