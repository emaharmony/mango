package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// --- Types ---

type Memory struct {
	ID             string   `json:"id"`
	Content        string   `json:"content"`
	Summary        string   `json:"summary,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	Category       string   `json:"category"`
	Tier           string   `json:"tier"`
	Source         string   `json:"source"`
	Confidence     float64  `json:"confidence"`
	ReferenceCount int      `json:"reference_count"`
	CreatedAt      string   `json:"created_at"`
	LastReferenced string   `json:"last_referenced"`
	LastTierChange string   `json:"last_tier_change"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
}

type SearchOpts struct {
	Category string `json:"category,omitempty"`
	Tier     string `json:"tier,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type MemoryStats struct {
	TierCounts    map[string]int `json:"tier_counts"`
	CategoryCount map[string]int `json:"category_counts"`
	Total         int            `json:"total"`
}

type DecayResult struct {
	DemotedToCold       int `json:"demoted_to_cold"`
	PromotedToActive    int `json:"promoted_to_active"`
	PromotedToPersistent int `json:"promoted_to_persistent"`
	ArchivedOrPruned    int `json:"archived_or_pruned"`
}

// --- Interfaces ---

type MemoryReader interface {
	Search(query string, opts SearchOpts) ([]Memory, error)
	Recall(id string) (Memory, error)
	Stats() (MemoryStats, error)
}

type Store interface {
	MemoryReader
	StoreMemory(content, category string, keywords []string, tier string) (string, error)
	Promote(id, tier string) error
	SetConfig(key, value string) error
	GetConfig(key string) (string, error)
	RunDecay() (DecayResult, error)
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	List(prefix string) (map[string]string, error)
	Close() error
}

// --- Implementation ---

type sqliteStore struct {
	db        *sql.DB
	path      string
	mu        sync.Mutex
	stopDecay chan struct{}
}

func Open(dir string) (Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	path := filepath.Join(dir, "memory.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	s := &sqliteStore{
		db:        db,
		path:      path,
		stopDecay: make(chan struct{}),
	}
	go s.decayLoop()
	return s, nil
}

func initSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create kv table: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		summary TEXT,
		keywords TEXT DEFAULT '[]',
		category TEXT NOT NULL,
		tier TEXT NOT NULL DEFAULT 'active',
		source TEXT DEFAULT 'manual',
		confidence REAL DEFAULT 1.0,
		reference_count INTEGER DEFAULT 0,
		created_at TEXT DEFAULT (datetime('now')),
		last_referenced TEXT DEFAULT (datetime('now')),
		last_tier_change TEXT DEFAULT (datetime('now')),
		expires_at TEXT
	)`); err != nil {
		return fmt.Errorf("create memories table: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS memory_references (
		id TEXT PRIMARY KEY,
		memory_id TEXT NOT NULL,
		referenced_at TEXT DEFAULT (datetime('now')),
		context TEXT,
		FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
	)`); err != nil {
		return fmt.Errorf("create memory_references table: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS decay_config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		description TEXT
	)`); err != nil {
		return fmt.Errorf("create decay_config table: %w", err)
	}
	defaults := []struct{ key, value, desc string }{
		{"active_to_cold_days", "30", "Days before active memory decays to cold"},
		{"cold_to_prune_days", "90", "Days before cold memory is pruned or archived"},
		{"archive_mode", "archive", "prune = delete, archive = export to .md then delete"},
		{"reference_promote_threshold", "3", "References needed to promote to next tier"},
		{"tracked_users", "[]", "Discord users whose conversations are auto-captured (empty by default)"},
		{"auto_capture", "true", "Automatically save important moments during conversation"},
		{"auto_inject", "true", "Inject relevant memories into agent context automatically"},
		{"decay_interval_hours", "168", "Hours between decay cycles (default: weekly)"},
	}
	for _, d := range defaults {
		db.Exec(`INSERT OR IGNORE INTO decay_config (key, value, description) VALUES (?, ?, ?)`, d.key, d.value, d.desc)
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories(tier)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_last_referenced ON memories(last_referenced)`,
		`CREATE INDEX IF NOT EXISTS idx_refs_memory ON memory_references(memory_id)`,
	} {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	return nil
}

// scanMemory is a helper to scan a memory row with nullable fields.
func scanMemory(scanner interface{ Scan(...interface{}) error }) (Memory, error) {
	var m Memory
	var kwJSON, summary, expiresAt sql.NullString
	if err := scanner.Scan(
		&m.ID, &m.Content, &summary, &kwJSON,
		&m.Category, &m.Tier, &m.Source, &m.Confidence,
		&m.ReferenceCount, &m.CreatedAt, &m.LastReferenced,
		&m.LastTierChange, &expiresAt,
	); err != nil {
		return m, err
	}
	if summary.Valid {
		m.Summary = summary.String
	}
	if expiresAt.Valid {
		m.ExpiresAt = expiresAt.String
	}
	if kwJSON.Valid {
		json.Unmarshal([]byte(kwJSON.String), &m.Keywords)
	}
	return m, nil
}

const memoryCols = `id, content, summary, keywords, category, tier, source, confidence, reference_count, created_at, last_referenced, last_tier_change, expires_at`

func (s *sqliteStore) StoreMemory(content, category string, keywords []string, tier string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("mem_%d_%s", time.Now().Unix(), randomHex(4))
	kwJSON, _ := json.Marshal(keywords)
	summary := content
	if len(summary) > 120 {
		summary = summary[:120]
	}
	_, err := s.db.Exec(`INSERT INTO memories (id, content, summary, keywords, category, tier, source) VALUES (?, ?, ?, ?, ?, ?, 'manual')`,
		id, content, summary, string(kwJSON), category, tier)
	if err != nil {
		return "", fmt.Errorf("store memory: %w", err)
	}
	return id, nil
}

func (s *sqliteStore) Search(query string, opts SearchOpts) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	q := fmt.Sprintf(`SELECT %s FROM memories WHERE (content LIKE ? OR keywords LIKE ?)`, memoryCols)
	args := []interface{}{"%" + query + "%", "%" + query + "%"}
	if opts.Category != "" {
		q += ` AND category = ?`
		args = append(args, opts.Category)
	}
	if opts.Tier != "" {
		q += ` AND tier = ?`
		args = append(args, opts.Tier)
	}
	q += ` ORDER BY CASE tier WHEN 'persistent' THEN 0 WHEN 'active' THEN 1 WHEN 'cold' THEN 2 END, last_referenced DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

func (s *sqliteStore) Recall(id string) (Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(fmt.Sprintf(`SELECT %s FROM memories WHERE id = ?`, memoryCols), id)
	m, err := scanMemory(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Memory{}, fmt.Errorf("memory %q not found", id)
		}
		return Memory{}, err
	}

	// Record reference and bump count
	refID := fmt.Sprintf("ref_%d_%s", time.Now().Unix(), randomHex(4))
	s.db.Exec(`INSERT INTO memory_references (id, memory_id, context) VALUES (?, ?, 'recall')`, refID, id)
	s.db.Exec(`UPDATE memories SET reference_count = reference_count + 1, last_referenced = datetime('now') WHERE id = ?`, id)
	m.ReferenceCount++

	// Check for promotion
	threshold := s.getConfigIntLocked("reference_promote_threshold")
	if m.Tier == "cold" && m.ReferenceCount >= threshold {
		s.db.Exec(`UPDATE memories SET tier = 'active', last_tier_change = datetime('now') WHERE id = ?`, id)
		m.Tier = "active"
	} else if m.Tier == "active" && m.ReferenceCount >= threshold {
		s.db.Exec(`UPDATE memories SET tier = 'persistent', last_tier_change = datetime('now') WHERE id = ?`, id)
		m.Tier = "persistent"
	}

	return m, nil
}

func (s *sqliteStore) Promote(id, tier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tier != "persistent" && tier != "active" && tier != "cold" {
		return fmt.Errorf("invalid tier %q", tier)
	}
	res, err := s.db.Exec(`UPDATE memories SET tier = ?, last_tier_change = datetime('now') WHERE id = ?`, tier, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory %q not found", id)
	}
	return nil
}

func (s *sqliteStore) Stats() (MemoryStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := MemoryStats{
		TierCounts:    map[string]int{"persistent": 0, "active": 0, "cold": 0},
		CategoryCount: map[string]int{},
	}
	rows, err := s.db.Query(`SELECT tier, category, count(*) FROM memories GROUP BY tier, category`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var tier, cat string
		var count int
		if err := rows.Scan(&tier, &cat, &count); err != nil {
			return stats, err
		}
		stats.TierCounts[tier] += count
		stats.CategoryCount[cat] += count
		stats.Total += count
	}
	return stats, rows.Err()
}

func (s *sqliteStore) SetConfig(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO decay_config (key, value, description) VALUES (?, ?, '') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *sqliteStore) GetConfig(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getConfigLocked(key)
}

func (s *sqliteStore) RunDecay() (DecayResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := DecayResult{}
	activeDays := s.getConfigIntLocked("active_to_cold_days")
	coldDays := s.getConfigIntLocked("cold_to_prune_days")
	archiveMode, _ := s.getConfigLocked("archive_mode")
	threshold := s.getConfigIntLocked("reference_promote_threshold")

	// Demote active → cold
	r, err := s.db.Exec(`UPDATE memories SET tier = 'cold', last_tier_change = datetime('now') WHERE tier = 'active' AND julianday('now') - julianday(last_referenced) > ?`, activeDays)
	if err != nil {
		return result, err
	}
	n, _ := r.RowsAffected()
	result.DemotedToCold = int(n)

	// Promote cold → active
	r, err = s.db.Exec(`UPDATE memories SET tier = 'active', last_tier_change = datetime('now') WHERE tier = 'cold' AND reference_count >= ?`, threshold)
	if err != nil {
		return result, err
	}
	n, _ = r.RowsAffected()
	result.PromotedToActive = int(n)

	// Promote active → persistent
	r, err = s.db.Exec(`UPDATE memories SET tier = 'persistent', last_tier_change = datetime('now') WHERE tier = 'active' AND reference_count >= ?`, threshold)
	if err != nil {
		return result, err
	}
	n, _ = r.RowsAffected()
	result.PromotedToPersistent = int(n)

	// Handle cold → archive/prune
	if archiveMode == "archive" {
		rows, err := s.db.Query(`SELECT id, content, category, keywords, created_at FROM memories WHERE tier = 'cold' AND julianday('now') - julianday(last_referenced) > ?`, coldDays)
		if err == nil {
			var archiveLines []string
			for rows.Next() {
				var id, content, category, kwJSON, createdAt string
				if rows.Scan(&id, &content, &category, &kwJSON, &createdAt) == nil {
					archiveLines = append(archiveLines, fmt.Sprintf("- [%s] (%s) %s {keywords: %s}", createdAt, category, content, kwJSON))
				}
			}
			rows.Close()
			if len(archiveLines) > 0 {
				f, err := os.OpenFile(filepath.Join(filepath.Dir(s.path), "archive.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err == nil {
					for _, line := range archiveLines {
						f.WriteString(line + "\n")
					}
					f.Close()
				}
			}
		}
	}

	r, err = s.db.Exec(`DELETE FROM memories WHERE tier = 'cold' AND julianday('now') - julianday(last_referenced) > ?`, coldDays)
	if err != nil {
		return result, err
	}
	n, _ = r.RowsAffected()
	result.ArchivedOrPruned = int(n)

	return result, nil
}

func (s *sqliteStore) decayLoop() {
	intervalHours := s.getConfigInt("decay_interval_hours")
	if intervalHours <= 0 {
		intervalHours = 168
	}
	ticker := time.NewTicker(time.Duration(intervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			result, err := s.RunDecay()
			if err != nil {
				log.Printf("memory decay error: %v", err)
			} else {
				log.Printf("memory decay complete: demoted=%d promoted_active=%d promoted_persistent=%d archived=%d",
					result.DemotedToCold, result.PromotedToActive, result.PromotedToPersistent, result.ArchivedOrPruned)
			}
		case <-s.stopDecay:
			return
		}
	}
}

// --- Existing kv Methods ---

func (s *sqliteStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *sqliteStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *sqliteStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM kv WHERE key = ?`, key)
	return err
}

func (s *sqliteStore) List(prefix string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT key, value FROM kv WHERE key LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *sqliteStore) Close() error {
	close(s.stopDecay)
	return s.db.Close()
}

// --- Helpers ---

func (s *sqliteStore) getConfigInt(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getConfigIntLocked(key)
}

func (s *sqliteStore) getConfigIntLocked(key string) int {
	val, _ := s.getConfigLocked(key)
	var n int
	fmt.Sscanf(val, "%d", &n)
	return n
}

func (s *sqliteStore) getConfigLocked(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM decay_config WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = "0123456789abcdef"[time.Now().UnixNano()%16]
	}
	return fmt.Sprintf("%x", b)
}

// ReadOnlyStore wraps a Store for non-manager agents.
type ReadOnlyStore struct {
	inner MemoryReader
	kv    Store
}

func NewReadOnlyStore(s Store) *ReadOnlyStore {
	return &ReadOnlyStore{inner: s, kv: s}
}

func (r *ReadOnlyStore) Search(query string, opts SearchOpts) ([]Memory, error) {
	return r.inner.Search(query, opts)
}

func (r *ReadOnlyStore) Recall(id string) (Memory, error) {
	return r.inner.Recall(id)
}

func (r *ReadOnlyStore) Stats() (MemoryStats, error) {
	return r.inner.Stats()
}

func (r *ReadOnlyStore) Get(key string) (string, error) {
	return r.kv.Get(key)
}

func (r *ReadOnlyStore) List(prefix string) (map[string]string, error) {
	return r.kv.List(prefix)
}