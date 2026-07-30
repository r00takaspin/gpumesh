package coord

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// APIKey represents a stored API key row.
type APIKey struct {
	ID        int64
	UserID    int64
	KeyHash   string
	KeyPrefix string
	Scope     string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// OwnerStats represents persistent owner/provider statistics.
type OwnerStats struct {
	UserID         int64
	TotalRequests  int64
	TotalTokens    int64
	TotalUptimeSec int64
	LastSeenAt     *time.Time
}

// Machine is a stable logical machine bound to a provider key.
type Machine struct {
	ID            string
	OwnerUserID   int64
	ProviderKeyID int64
	DisplayName   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Invite is a PIN invite for a machine.
type Invite struct {
	ID          int64
	MachineID   string
	OwnerUserID int64
	PinHash     string
	PinPrefix   string
	MaxUses     int
	Uses        int
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	Label       string
}

// Binding grants a member access to a machine.
type Binding struct {
	ID           int64
	MachineID    string
	MemberUserID int64
	InviteID     *int64
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

// BindingInfo is a machine accessible to a user (owned or member).
type BindingInfo struct {
	MachineID   string
	DisplayName string
	OwnerUserID int64
	Role        string // "owner" or "member"
	CreatedAt   time.Time
}

// Redeem errors (§4.4 SPEC-v2).
var (
	ErrInvalidPin  = errors.New("invalid_pin")
	ErrExpiredPin  = errors.New("expired")
	ErrExhausted   = errors.New("exhausted")
	ErrRevokedPin  = errors.New("revoked")
	ErrMachineGone = errors.New("machine_gone")
)

// PIN alphabet without 0/O/1/I/L.
const pinAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// Store manages persistent state via SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) the SQLite database and runs migrations.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		github_id INTEGER UNIQUE NOT NULL,
		github_login TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		key_hash TEXT UNIQUE NOT NULL,
		key_prefix TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'consumer',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		revoked_at TEXT
	);

	CREATE TABLE IF NOT EXISTS owner_stats (
		user_id INTEGER PRIMARY KEY REFERENCES users(id),
		total_requests INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		total_uptime_seconds INTEGER NOT NULL DEFAULT 0,
		last_seen_at TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		expires_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS machines (
		id TEXT PRIMARY KEY,
		owner_user_id INTEGER NOT NULL REFERENCES users(id),
		provider_key_id INTEGER NOT NULL UNIQUE REFERENCES api_keys(id),
		display_name TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS invites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_id TEXT NOT NULL REFERENCES machines(id),
		owner_user_id INTEGER NOT NULL REFERENCES users(id),
		pin_hash TEXT UNIQUE NOT NULL,
		pin_prefix TEXT NOT NULL,
		max_uses INTEGER NOT NULL DEFAULT 1,
		uses INTEGER NOT NULL DEFAULT 0,
		expires_at TEXT NOT NULL,
		revoked_at TEXT,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		label TEXT
	);

	CREATE TABLE IF NOT EXISTS bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_id TEXT NOT NULL REFERENCES machines(id),
		member_user_id INTEGER NOT NULL REFERENCES users(id),
		invite_id INTEGER REFERENCES invites(id),
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		revoked_at TEXT,
		UNIQUE(machine_id, member_user_id)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migrate donor_stats → owner_stats if legacy table exists.
	var donorExists int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='donor_stats'`,
	).Scan(&donorExists)
	if donorExists > 0 {
		_, _ = db.Exec(`
			INSERT OR IGNORE INTO owner_stats (user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at)
			SELECT user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at FROM donor_stats`)
		_, _ = db.Exec(`DROP TABLE donor_stats`)
	}

	// Migrate scope donor → provider.
	_, _ = db.Exec(`UPDATE api_keys SET scope = 'provider' WHERE scope = 'donor'`)

	return nil
}

// UpsertUser inserts a user if not present or updates login, returns the user ID.
func (s *Store) UpsertUser(githubID int64, login string) (int64, error) {
	_, err := s.db.Exec(
		`INSERT INTO users (github_id, github_login) VALUES (?, ?)
		 ON CONFLICT(github_id) DO UPDATE SET github_login = excluded.github_login`,
		githubID, login,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert user: %w", err)
	}

	var id int64
	err = s.db.QueryRow(`SELECT id FROM users WHERE github_id = ?`, githubID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("select user: %w", err)
	}
	return id, nil
}

// CreateKey generates a new API key, stores its SHA-256 hash, and returns the raw key.
func (s *Store) CreateKey(userID int64, scope string) (rawKey string, keyID int64, err error) {
	scope = normalizeScope(scope)
	raw, err := generateAPIKey()
	if err != nil {
		return "", 0, fmt.Errorf("generate key: %w", err)
	}

	hash := hashKey(raw)
	prefix := raw[:8]

	res, err := s.db.Exec(
		`INSERT INTO api_keys (user_id, key_hash, key_prefix, scope) VALUES (?, ?, ?, ?)`,
		userID, hash, prefix, scope,
	)
	if err != nil {
		return "", 0, fmt.Errorf("insert key: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return "", 0, fmt.Errorf("last insert id: %w", err)
	}
	return raw, id, nil
}

func normalizeScope(scope string) string {
	if scope == "donor" {
		return "provider"
	}
	return scope
}

// ListKeys returns all non-revoked API keys for a user.
func (s *Store) ListKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, key_hash, key_prefix, scope, created_at, revoked_at
		 FROM api_keys WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var createdStr string
		var revokedStr *string
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.KeyPrefix, &k.Scope, &createdStr, &revokedStr); err != nil {
			return nil, fmt.Errorf("scan key: %w", err)
		}
		k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if revokedStr != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *revokedStr)
			k.RevokedAt = &t
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// CountKeys returns the number of non-revoked API keys for a user.
func (s *Store) CountKeys(userID int64) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL`,
		userID,
	).Scan(&count)
	return count, err
}

// CountKeysByScope returns the number of non-revoked API keys for a user with a given scope.
// Counts both "provider" and legacy "donor" when asking for provider.
func (s *Store) CountKeysByScope(userID int64, scope string) (int, error) {
	scope = normalizeScope(scope)
	var count int
	var err error
	if scope == "provider" {
		err = s.db.QueryRow(
			`SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND scope IN ('provider','donor') AND revoked_at IS NULL`,
			userID,
		).Scan(&count)
	} else {
		err = s.db.QueryRow(
			`SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND scope = ? AND revoked_at IS NULL`,
			userID, scope,
		).Scan(&count)
	}
	return count, err
}

// RevokeKey marks a key as revoked. Only the owner can revoke.
func (s *Store) RevokeKey(userID, keyID int64) error {
	res, err := s.db.Exec(
		`UPDATE api_keys SET revoked_at = datetime('now')
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("key not found or already revoked")
	}
	return nil
}

// FindKeyByHash looks up an API key by its SHA-256 hash.
func (s *Store) FindKeyByHash(hash string) (*APIKey, error) {
	var k APIKey
	var createdStr string
	var revokedStr *string
	err := s.db.QueryRow(
		`SELECT id, user_id, key_hash, key_prefix, scope, created_at, revoked_at
		 FROM api_keys WHERE key_hash = ?`, hash,
	).Scan(&k.ID, &k.UserID, &k.KeyHash, &k.KeyPrefix, &k.Scope, &createdStr, &revokedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find key: %w", err)
	}
	k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	if revokedStr != nil {
		return nil, nil
	}
	k.Scope = normalizeScope(k.Scope)
	return &k, nil
}

// FindKeyByID looks up a key by its ID.
func (s *Store) FindKeyByID(keyID int64) (*APIKey, error) {
	var k APIKey
	var createdStr string
	var revokedStr *string
	err := s.db.QueryRow(
		`SELECT id, user_id, key_hash, key_prefix, scope, created_at, revoked_at
		 FROM api_keys WHERE id = ?`, keyID,
	).Scan(&k.ID, &k.UserID, &k.KeyHash, &k.KeyPrefix, &k.Scope, &createdStr, &revokedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find key by id: %w", err)
	}
	k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	if revokedStr != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *revokedStr)
		k.RevokedAt = &t
	}
	k.Scope = normalizeScope(k.Scope)
	return &k, nil
}

// GetOwnerStats returns persistent owner statistics for a user.
func (s *Store) GetOwnerStats(userID int64) (*OwnerStats, error) {
	var ds OwnerStats
	var lastSeen *string
	err := s.db.QueryRow(
		`SELECT user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at
		 FROM owner_stats WHERE user_id = ?`, userID,
	).Scan(&ds.UserID, &ds.TotalRequests, &ds.TotalTokens, &ds.TotalUptimeSec, &lastSeen)
	if err == sql.ErrNoRows {
		return &OwnerStats{UserID: userID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get owner stats: %w", err)
	}
	if lastSeen != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *lastSeen)
		ds.LastSeenAt = &t
	}
	return &ds, nil
}

// UpdateOwnerStats increments persistent owner counters.
func (s *Store) UpdateOwnerStats(userID int64, requests, tokens, uptimeSec int64) error {
	_, err := s.db.Exec(
		`INSERT INTO owner_stats (user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at)
		 VALUES (?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   total_requests = total_requests + excluded.total_requests,
		   total_tokens = total_tokens + excluded.total_tokens,
		   total_uptime_seconds = total_uptime_seconds + excluded.total_uptime_seconds,
		   last_seen_at = excluded.last_seen_at`,
		userID, requests, tokens, uptimeSec,
	)
	if err != nil {
		return fmt.Errorf("update owner stats: %w", err)
	}
	return nil
}

// --- Machines ---

// UpsertMachineByProviderKey returns the stable machine for a provider key, creating one if needed.
func (s *Store) UpsertMachineByProviderKey(ownerUserID, providerKeyID int64, displayName string) (*Machine, error) {
	existing, err := s.GetMachineByProviderKeyID(providerKeyID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if displayName != "" && displayName != existing.DisplayName {
			_, _ = s.db.Exec(
				`UPDATE machines SET display_name = ?, updated_at = datetime('now') WHERE id = ?`,
				displayName, existing.ID,
			)
			existing.DisplayName = displayName
		}
		return existing, nil
	}

	id, err := generateMachineID()
	if err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = "machine"
	}
	_, err = s.db.Exec(
		`INSERT INTO machines (id, owner_user_id, provider_key_id, display_name)
		 VALUES (?, ?, ?, ?)`,
		id, ownerUserID, providerKeyID, displayName,
	)
	if err != nil {
		// Race: another connection may have created it.
		existing, err2 := s.GetMachineByProviderKeyID(providerKeyID)
		if err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("insert machine: %w", err)
	}
	return s.GetMachine(id)
}

// CreateMachineOnKeyRegen creates a new machine row for a regenerated provider key.
func (s *Store) CreateMachineOnKeyRegen(ownerUserID, newProviderKeyID int64, displayName string) (*Machine, error) {
	return s.UpsertMachineByProviderKey(ownerUserID, newProviderKeyID, displayName)
}

// GetMachine returns a machine by id.
func (s *Store) GetMachine(id string) (*Machine, error) {
	var m Machine
	var createdStr, updatedStr string
	err := s.db.QueryRow(
		`SELECT id, owner_user_id, provider_key_id, display_name, created_at, updated_at
		 FROM machines WHERE id = ?`, id,
	).Scan(&m.ID, &m.OwnerUserID, &m.ProviderKeyID, &m.DisplayName, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get machine: %w", err)
	}
	m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
	return &m, nil
}

// GetMachineByProviderKeyID returns the machine for a provider key, if any.
func (s *Store) GetMachineByProviderKeyID(keyID int64) (*Machine, error) {
	var m Machine
	var createdStr, updatedStr string
	err := s.db.QueryRow(
		`SELECT id, owner_user_id, provider_key_id, display_name, created_at, updated_at
		 FROM machines WHERE provider_key_id = ?`, keyID,
	).Scan(&m.ID, &m.OwnerUserID, &m.ProviderKeyID, &m.DisplayName, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get machine by key: %w", err)
	}
	m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
	return &m, nil
}

// ListMachinesByOwner returns machines owned by a user whose provider key is still active.
// Machines tied to a revoked/regenerated provider key are omitted (SPEC-v2 §3.3 / §4.5).
func (s *Store) ListMachinesByOwner(ownerUserID int64) ([]Machine, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.owner_user_id, m.provider_key_id, m.display_name, m.created_at, m.updated_at
		 FROM machines m
		 JOIN api_keys k ON k.id = m.provider_key_id
		 WHERE m.owner_user_id = ? AND k.revoked_at IS NULL
		 ORDER BY m.created_at DESC`,
		ownerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Machine
	for rows.Next() {
		var m Machine
		var createdStr, updatedStr string
		if err := rows.Scan(&m.ID, &m.OwnerUserID, &m.ProviderKeyID, &m.DisplayName, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
		out = append(out, m)
	}
	return out, rows.Err()
}

// CanAccessMachine returns true if user is owner (with active provider key) or has an active binding.
func (s *Store) CanAccessMachine(userID int64, machineID string) (bool, error) {
	m, err := s.GetMachine(machineID)
	if err != nil {
		return false, err
	}
	if m == nil {
		return false, nil
	}
	if m.OwnerUserID == userID {
		var revoked sql.NullString
		err = s.db.QueryRow(`SELECT revoked_at FROM api_keys WHERE id = ?`, m.ProviderKeyID).Scan(&revoked)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return !revoked.Valid, nil
	}
	var n int
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM bindings
		 WHERE machine_id = ? AND member_user_id = ? AND revoked_at IS NULL`,
		machineID, userID,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RetireMachine invalidates invites and bindings for a machine (used when its provider key is revoked/regenerated).
func (s *Store) RetireMachine(machineID string) error {
	if _, err := s.db.Exec(
		`UPDATE invites SET revoked_at = datetime('now')
		 WHERE machine_id = ? AND revoked_at IS NULL`, machineID,
	); err != nil {
		return fmt.Errorf("retire invites: %w", err)
	}
	if _, err := s.db.Exec(
		`UPDATE bindings SET revoked_at = datetime('now')
		 WHERE machine_id = ? AND revoked_at IS NULL`, machineID,
	); err != nil {
		return fmt.Errorf("retire bindings: %w", err)
	}
	return nil
}

// RetireMachineByProviderKeyID retires the machine (if any) bound to a provider key.
func (s *Store) RetireMachineByProviderKeyID(keyID int64) error {
	m, err := s.GetMachineByProviderKeyID(keyID)
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	return s.RetireMachine(m.ID)
}

// --- Invites ---

// CreateInvite creates a new invite and returns the invite plus plaintext PIN (once).
func (s *Store) CreateInvite(machineID string, ownerUserID int64, maxUses, ttlDays int, label string) (*Invite, string, error) {
	m, err := s.GetMachine(machineID)
	if err != nil {
		return nil, "", err
	}
	if m == nil || m.OwnerUserID != ownerUserID {
		return nil, "", fmt.Errorf("machine not found or not owned")
	}
	if maxUses < 1 {
		maxUses = 1
	}
	if maxUses > 10 {
		maxUses = 10
	}
	if ttlDays < 1 {
		ttlDays = 7
	}

	pin, err := generatePIN()
	if err != nil {
		return nil, "", err
	}
	pinHash := hashPIN(pin)
	prefix := pin[:4]

	expiresAt := time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour)
	res, err := s.db.Exec(
		`INSERT INTO invites (machine_id, owner_user_id, pin_hash, pin_prefix, max_uses, uses, expires_at, label)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		machineID, ownerUserID, pinHash, prefix, maxUses,
		expiresAt.Format("2006-01-02 15:04:05"), nullIfEmpty(label),
	)
	if err != nil {
		return nil, "", fmt.Errorf("insert invite: %w", err)
	}
	id, _ := res.LastInsertId()
	inv, err := s.GetInvite(id)
	if err != nil {
		return nil, "", err
	}
	return inv, pin, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// GetInvite returns an invite by id.
func (s *Store) GetInvite(id int64) (*Invite, error) {
	var inv Invite
	var expiresStr, createdStr string
	var revokedStr, label *string
	err := s.db.QueryRow(
		`SELECT id, machine_id, owner_user_id, pin_hash, pin_prefix, max_uses, uses, expires_at, revoked_at, created_at, label
		 FROM invites WHERE id = ?`, id,
	).Scan(&inv.ID, &inv.MachineID, &inv.OwnerUserID, &inv.PinHash, &inv.PinPrefix,
		&inv.MaxUses, &inv.Uses, &expiresStr, &revokedStr, &createdStr, &label)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invite: %w", err)
	}
	inv.ExpiresAt, _ = time.Parse("2006-01-02 15:04:05", expiresStr)
	inv.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	if revokedStr != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *revokedStr)
		inv.RevokedAt = &t
	}
	if label != nil {
		inv.Label = *label
	}
	return &inv, nil
}

// ListInviteRedeemers returns github logins that redeemed each invite (including later-revoked bindings).
func (s *Store) ListInviteRedeemers(inviteIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(inviteIDs))
	if len(inviteIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(inviteIDs))
	args := make([]interface{}, len(inviteIDs))
	for i, id := range inviteIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT b.invite_id, u.github_login
		 FROM bindings b
		 JOIN users u ON u.id = b.member_user_id
		 WHERE b.invite_id IN (` + strings.Join(placeholders, ",") + `)
		 ORDER BY b.created_at ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list invite redeemers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var inviteID int64
		var login string
		if err := rows.Scan(&inviteID, &login); err != nil {
			return nil, err
		}
		out[inviteID] = append(out[inviteID], login)
	}
	return out, rows.Err()
}

// ListInvitesByOwner returns invites created by the owner.
func (s *Store) ListInvitesByOwner(ownerUserID int64) ([]Invite, error) {
	rows, err := s.db.Query(
		`SELECT id, machine_id, owner_user_id, pin_hash, pin_prefix, max_uses, uses, expires_at, revoked_at, created_at, label
		 FROM invites WHERE owner_user_id = ? ORDER BY created_at DESC`,
		ownerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Invite
	for rows.Next() {
		var inv Invite
		var expiresStr, createdStr string
		var revokedStr, label *string
		if err := rows.Scan(&inv.ID, &inv.MachineID, &inv.OwnerUserID, &inv.PinHash, &inv.PinPrefix,
			&inv.MaxUses, &inv.Uses, &expiresStr, &revokedStr, &createdStr, &label); err != nil {
			return nil, err
		}
		inv.ExpiresAt, _ = time.Parse("2006-01-02 15:04:05", expiresStr)
		inv.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if revokedStr != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *revokedStr)
			inv.RevokedAt = &t
		}
		if label != nil {
			inv.Label = *label
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite marks an invite as revoked.
func (s *Store) RevokeInvite(ownerUserID, inviteID int64) error {
	res, err := s.db.Exec(
		`UPDATE invites SET revoked_at = datetime('now')
		 WHERE id = ? AND owner_user_id = ? AND revoked_at IS NULL`,
		inviteID, ownerUserID,
	)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("invite not found or already revoked")
	}
	return nil
}

// RedeemPIN creates/reactivates a binding for the member. Returns machine_id and whether a new consumer key is needed.
func (s *Store) RedeemPIN(memberUserID int64, pin string) (machineID string, inviteID int64, err error) {
	pin = normalizePIN(pin)
	if !validPINFormat(pin) {
		return "", 0, ErrInvalidPin
	}
	pinHash := hashPIN(pin)

	var inv Invite
	var expiresStr string
	var revokedStr *string
	err = s.db.QueryRow(
		`SELECT id, machine_id, owner_user_id, pin_hash, pin_prefix, max_uses, uses, expires_at, revoked_at
		 FROM invites WHERE pin_hash = ?`, pinHash,
	).Scan(&inv.ID, &inv.MachineID, &inv.OwnerUserID, &inv.PinHash, &inv.PinPrefix,
		&inv.MaxUses, &inv.Uses, &expiresStr, &revokedStr)
	if err == sql.ErrNoRows {
		return "", 0, ErrInvalidPin
	}
	if err != nil {
		return "", 0, fmt.Errorf("lookup pin: %w", err)
	}
	if revokedStr != nil {
		return "", 0, ErrRevokedPin
	}
	inv.ExpiresAt, _ = time.Parse("2006-01-02 15:04:05", expiresStr)
	if time.Now().UTC().After(inv.ExpiresAt) {
		return "", 0, ErrExpiredPin
	}
	if inv.Uses >= inv.MaxUses {
		return "", 0, ErrExhausted
	}

	m, err := s.GetMachine(inv.MachineID)
	if err != nil {
		return "", 0, err
	}
	if m == nil {
		return "", 0, ErrMachineGone
	}

	// Owner redeem: no-op binding, still count as success.
	if m.OwnerUserID == memberUserID {
		_, _ = s.db.Exec(`UPDATE invites SET uses = uses + 1 WHERE id = ? AND uses < max_uses`, inv.ID)
		return inv.MachineID, inv.ID, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`UPDATE invites SET uses = uses + 1 WHERE id = ? AND uses < max_uses AND revoked_at IS NULL`, inv.ID)
	if err != nil {
		return "", 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", 0, ErrExhausted
	}

	_, err = tx.Exec(
		`INSERT INTO bindings (machine_id, member_user_id, invite_id)
		 VALUES (?, ?, ?)
		 ON CONFLICT(machine_id, member_user_id) DO UPDATE SET
		   revoked_at = NULL,
		   invite_id = excluded.invite_id,
		   created_at = CASE WHEN bindings.revoked_at IS NOT NULL THEN datetime('now') ELSE bindings.created_at END`,
		inv.MachineID, memberUserID, inv.ID,
	)
	if err != nil {
		return "", 0, fmt.Errorf("upsert binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return inv.MachineID, inv.ID, nil
}

// --- Bindings ---

// ListAccessibleMachines returns owned + member machines for a user.
func (s *Store) ListAccessibleMachines(userID int64) ([]BindingInfo, error) {
	var out []BindingInfo

	owned, err := s.ListMachinesByOwner(userID)
	if err != nil {
		return nil, err
	}
	for _, m := range owned {
		out = append(out, BindingInfo{
			MachineID:   m.ID,
			DisplayName: m.DisplayName,
			OwnerUserID: m.OwnerUserID,
			Role:        "owner",
			CreatedAt:   m.CreatedAt,
		})
	}

	rows, err := s.db.Query(
		`SELECT b.machine_id, m.display_name, m.owner_user_id, b.created_at
		 FROM bindings b
		 JOIN machines m ON m.id = b.machine_id
		 JOIN api_keys k ON k.id = m.provider_key_id
		 WHERE b.member_user_id = ? AND b.revoked_at IS NULL AND k.revoked_at IS NULL
		 ORDER BY b.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var bi BindingInfo
		var createdStr string
		if err := rows.Scan(&bi.MachineID, &bi.DisplayName, &bi.OwnerUserID, &createdStr); err != nil {
			return nil, err
		}
		bi.Role = "member"
		bi.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		out = append(out, bi)
	}
	return out, rows.Err()
}

// ListMembers returns active member bindings for a machine (owner only).
func (s *Store) ListMembers(machineID string, ownerUserID int64) ([]Binding, error) {
	m, err := s.GetMachine(machineID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.OwnerUserID != ownerUserID {
		return nil, fmt.Errorf("machine not found or not owned")
	}
	rows, err := s.db.Query(
		`SELECT id, machine_id, member_user_id, invite_id, created_at, revoked_at
		 FROM bindings WHERE machine_id = ? AND revoked_at IS NULL`,
		machineID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Binding
	for rows.Next() {
		var b Binding
		var createdStr string
		var revokedStr *string
		var inviteID sql.NullInt64
		if err := rows.Scan(&b.ID, &b.MachineID, &b.MemberUserID, &inviteID, &createdStr, &revokedStr); err != nil {
			return nil, err
		}
		if inviteID.Valid {
			id := inviteID.Int64
			b.InviteID = &id
		}
		b.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		out = append(out, b)
	}
	return out, rows.Err()
}

// RevokeBindingByMember lets a member remove their own binding.
func (s *Store) RevokeBindingByMember(memberUserID int64, machineID string) error {
	res, err := s.db.Exec(
		`UPDATE bindings SET revoked_at = datetime('now')
		 WHERE machine_id = ? AND member_user_id = ? AND revoked_at IS NULL`,
		machineID, memberUserID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("binding not found")
	}
	return nil
}

// RevokeMemberByOwner lets an owner revoke a member's access.
func (s *Store) RevokeMemberByOwner(ownerUserID int64, machineID string, memberUserID int64) error {
	m, err := s.GetMachine(machineID)
	if err != nil {
		return err
	}
	if m == nil || m.OwnerUserID != ownerUserID {
		return fmt.Errorf("machine not found or not owned")
	}
	res, err := s.db.Exec(
		`UPDATE bindings SET revoked_at = datetime('now')
		 WHERE machine_id = ? AND member_user_id = ? AND revoked_at IS NULL`,
		machineID, memberUserID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("binding not found")
	}
	return nil
}

// EnsureConsumerKey returns an existing consumer/both key or creates a consumer key.
// Returns rawKey only when newly created (empty string if reused).
func (s *Store) EnsureConsumerKey(userID int64) (rawKey string, keyID int64, created bool, err error) {
	keys, err := s.ListKeys(userID)
	if err != nil {
		return "", 0, false, err
	}
	for _, k := range keys {
		if k.Scope == "consumer" || k.Scope == "both" {
			return "", k.ID, false, nil
		}
	}
	raw, id, err := s.CreateKey(userID, "consumer")
	if err != nil {
		return "", 0, false, err
	}
	return raw, id, true, nil
}

// --- Session management ---

func (s *Store) CreateSession(userID int64) (token string, err error) {
	token, err = generateSessionToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(
		`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, datetime('now', '+24 hours'))`,
		token, userID,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func (s *Store) ValidateSession(token string) (int64, error) {
	var userID int64
	err := s.db.QueryRow(
		`SELECT user_id FROM sessions WHERE token = ? AND expires_at > datetime('now')`,
		token,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("validate session: %w", err)
	}
	return userID, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) GetUserByID(userID int64) (githubLogin string, err error) {
	err = s.db.QueryRow(`SELECT github_login FROM users WHERE id = ?`, userID).Scan(&githubLogin)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("user not found")
	}
	return githubLogin, err
}

// --- Helpers ---

func generateAPIKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "inf_" + hex.EncodeToString(b), nil
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateMachineID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mch_" + hex.EncodeToString(b), nil
}

func generatePIN() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	chars := make([]byte, 8)
	for i := range chars {
		chars[i] = pinAlphabet[int(b[i])%len(pinAlphabet)]
	}
	return string(chars[:4]) + "-" + string(chars[4:]), nil
}

func hashPIN(pin string) string {
	h := sha256.Sum256([]byte(normalizePIN(pin)))
	return hex.EncodeToString(h[:])
}

func normalizePIN(pin string) string {
	pin = strings.TrimSpace(strings.ToUpper(pin))
	pin = strings.ReplaceAll(pin, " ", "")
	return pin
}

func validPINFormat(pin string) bool {
	pin = normalizePIN(pin)
	if len(pin) != 9 || pin[4] != '-' {
		return false
	}
	for i, c := range pin {
		if i == 4 {
			continue
		}
		if !strings.ContainsRune(pinAlphabet, c) {
			return false
		}
	}
	return true
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// InviteStatus returns active/exhausted/expired/revoked.
func (inv *Invite) Status() string {
	if inv.RevokedAt != nil {
		return "revoked"
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return "expired"
	}
	if inv.Uses >= inv.MaxUses {
		return "exhausted"
	}
	return "active"
}

// MaskedPIN returns e.g. 7K4Q-****.
func (inv *Invite) MaskedPIN() string {
	return inv.PinPrefix + "-****"
}
