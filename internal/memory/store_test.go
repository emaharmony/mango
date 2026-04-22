package memory

import (
	"encoding/json"
	"testing"
)

// --- Existing kv tests (unchanged) ---

func TestStore_SetGet(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("k")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v" {
		t.Errorf("got %q, want v", got)
	}
}

func TestStore_GetMissingReturnsEmpty(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	got, err := s.Get("nope")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestStore_SetOverwrites(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	_ = s.Set("k", "v1")
	_ = s.Set("k", "v2")
	got, _ := s.Get("k")
	if got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestStore_Delete(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	_ = s.Set("k", "v")
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("k")
	if got != "" {
		t.Errorf("expected empty after delete, got %q", got)
	}
}

func TestStore_ListByPrefix(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	_ = s.Set("a/1", "1")
	_ = s.Set("a/2", "2")
	_ = s.Set("b/1", "3")

	got, err := s.List("a/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	if got["a/1"] != "1" || got["a/2"] != "2" {
		t.Errorf("wrong values: %+v", got)
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Set("persist", "yes")
	s.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, _ := s2.Get("persist")
	if got != "yes" {
		t.Errorf("expected persisted value, got %q", got)
	}
}

// --- New tiered memory tests ---

func TestStoreMemory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, err := s.StoreMemory("Ema prefers more questions before implementation", "preference", []string{"architecture", "questions"}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestSearchMemory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.StoreMemory("BassBook uses ASP.NET Core", "project", []string{"bassbook", "aspnet"}, "persistent")
	s.StoreMemory("Daeling is a Unity RPG", "project", []string{"daeling", "unity"}, "persistent")

	results, err := s.Search("BassBook", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "BassBook uses ASP.NET Core" {
		t.Errorf("wrong content: %q", results[0].Content)
	}
	if results[0].Tier != "persistent" {
		t.Errorf("wrong tier: %q", results[0].Tier)
	}
}

func TestRecallMemory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, _ := s.StoreMemory("Test memory for recall", "conversation", []string{"test"}, "active")

	m, err := s.Recall(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.ReferenceCount != 1 {
		t.Errorf("expected reference_count=1, got %d", m.ReferenceCount)
	}

	// Recall again — should bump
	m, _ = s.Recall(id)
	if m.ReferenceCount != 2 {
		t.Errorf("expected reference_count=2, got %d", m.ReferenceCount)
	}
}

func TestPromoteMemory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, _ := s.StoreMemory("Cold memory", "conversation", []string{"test"}, "cold")

	if err := s.Promote(id, "active"); err != nil {
		t.Fatal(err)
	}

	m, _ := s.Recall(id)
	if m.Tier != "active" {
		t.Errorf("expected tier=active, got %q", m.Tier)
	}
}

func TestMemoryStats(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.StoreMemory("Persistent one", "project", []string{"test"}, "persistent")
	s.StoreMemory("Active one", "conversation", []string{"test"}, "active")
	s.StoreMemory("Cold one", "decision", []string{"test"}, "cold")

	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 {
		t.Errorf("expected total=3, got %d", stats.Total)
	}
	// Note: Recall in promote test may bump reference counts,
	// but each test uses a separate DB so tier counts should be accurate
	if stats.TierCounts["persistent"] < 1 {
		t.Errorf("expected persistent>=1, got %d", stats.TierCounts["persistent"])
	}
	if stats.TierCounts["active"] < 1 {
		t.Errorf("expected active>=1, got %d", stats.TierCounts["active"])
	}
}

func TestDecayConfig(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Check defaults
	val, err := s.GetConfig("active_to_cold_days")
	if err != nil {
		t.Fatal(err)
	}
	if val != "30" {
		t.Errorf("expected default active_to_cold_days=30, got %q", val)
	}

	// Set new value
	if err := s.SetConfig("active_to_cold_days", "14"); err != nil {
		t.Fatal(err)
	}
	val, _ = s.GetConfig("active_to_cold_days")
	if val != "14" {
		t.Errorf("expected active_to_cold_days=14, got %q", val)
	}
}

func TestTrackedUsersDefault(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	val, _ := s.GetConfig("tracked_users")
	if val != "[]" {
		t.Errorf("expected tracked_users default='[]', got %q", val)
	}

	// Set tracked users
	users := `[{"id":"123","name":"TestUser"}]`
	s.SetConfig("tracked_users", users)
	val, _ = s.GetConfig("tracked_users")
	var parsed []map[string]string
	if err := json.Unmarshal([]byte(val), &parsed); err != nil {
		t.Fatalf("tracked_users should be valid JSON: %v", err)
	}
	if len(parsed) != 1 || parsed[0]["name"] != "TestUser" {
		t.Errorf("unexpected tracked_users value: %q", val)
	}
}

func TestReadOnlyStore(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.StoreMemory("Test", "conversation", []string{"test"}, "active")

	ro := NewReadOnlyStore(s)

	// Read operations should work
	results, err := ro.Search("Test", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	// kv read should work
	s.Set("key", "value")
	got, _ := ro.Get("key")
	if got != "value" {
		t.Errorf("expected value, got %q", got)
	}
}