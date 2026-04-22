package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/carlosmaranje/mango/internal/memory"
)

func setupTestStore(t *testing.T) memory.Store {
	t.Helper()
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestMemorySearchTool(t *testing.T) {
	store := setupTestStore(t)
	tool := NewMemorySearchTool(store)

	// Store some memories
	store.StoreMemory("BassBook uses ASP.NET Core", "project", []string{"bassbook", "aspnet"}, "persistent")
	store.StoreMemory("Ema prefers more questions before implementation", "preference", []string{"architecture", "questions"}, "persistent")

	t.Run("search finds matching memory", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"query": "BassBook"}`)
		if err != nil {
			t.Fatal(err)
		}
		var searchResult MemorySearchResult
		if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
			t.Fatal(err)
		}
		if searchResult.Total != 1 {
			t.Errorf("expected 1 result, got %d", searchResult.Total)
		}
		if searchResult.Results[0].Category != "project" {
			t.Errorf("expected category=project, got %s", searchResult.Results[0].Category)
		}
	})

	t.Run("search with category filter", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"query": "prefers", "category": "preference"}`)
		if err != nil {
			t.Fatal(err)
		}
		var searchResult MemorySearchResult
		if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
			t.Fatal(err)
		}
		if searchResult.Total != 1 {
			t.Errorf("expected 1 result, got %d", searchResult.Total)
		}
	})

	t.Run("search with no results", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"query": "nonexistent"}`)
		if err != nil {
			t.Fatal(err)
		}
		var searchResult MemorySearchResult
		if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
			t.Fatal(err)
		}
		if searchResult.Total != 0 {
			t.Errorf("expected 0 results, got %d", searchResult.Total)
		}
	})

	t.Run("search with missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), `{}`)
		if err == nil {
			t.Error("expected error for missing query")
		}
	})
}

func TestMemoryRecallTool(t *testing.T) {
	store := setupTestStore(t)
	tool := NewMemoryRecallTool(store)

	id, _ := store.StoreMemory("Test memory for recall", "conversation", []string{"test"}, "active")

	t.Run("recall existing memory", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"id": "`+id+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		var recallResult MemoryRecallResult
		if err := json.Unmarshal([]byte(result), &recallResult); err != nil {
			t.Fatal(err)
		}
		if recallResult.Content != "Test memory for recall" {
			t.Errorf("expected content, got %q", recallResult.Content)
		}
		if recallResult.ReferenceCount != 1 {
			t.Errorf("expected reference_count=1, got %d", recallResult.ReferenceCount)
		}
	})

	t.Run("recall nonexistent memory", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), `{"id": "nonexistent"}`)
		if err == nil {
			t.Error("expected error for nonexistent id")
		}
	})
}

func TestMemoryStoreTool(t *testing.T) {
	store := setupTestStore(t)
	tool := NewMemoryStoreTool(store)

	t.Run("store a memory", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"content": "New test memory", "category": "project", "keywords": "test,memory", "tier": "active"}`)
		if err != nil {
			t.Fatal(err)
		}
		var storeResult MemoryStoreResult
		if err := json.Unmarshal([]byte(result), &storeResult); err != nil {
			t.Fatal(err)
		}
		if storeResult.ID == "" {
			t.Error("expected non-empty id")
		}
		if storeResult.Category != "project" {
			t.Errorf("expected category=project, got %s", storeResult.Category)
		}
		if len(storeResult.Keywords) != 2 {
			t.Errorf("expected 2 keywords, got %d", len(storeResult.Keywords))
		}
	})

	t.Run("store with defaults", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"content": "Another memory", "category": "conversation"}`)
		if err != nil {
			t.Fatal(err)
		}
		var storeResult MemoryStoreResult
		if err := json.Unmarshal([]byte(result), &storeResult); err != nil {
			t.Fatal(err)
		}
		if storeResult.Tier != "active" {
			t.Errorf("expected default tier=active, got %s", storeResult.Tier)
		}
	})

	t.Run("store missing content", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), `{"category": "project"}`)
		if err == nil {
			t.Error("expected error for missing content")
		}
	})
}

func TestMemoryConfigTool(t *testing.T) {
	store := setupTestStore(t)
	tool := NewMemoryConfigTool(store)

	t.Run("list all config", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{}`)
		if err != nil {
			t.Fatal(err)
		}
		var configResult MemoryConfigResult
		if err := json.Unmarshal([]byte(result), &configResult); err != nil {
			t.Fatal(err)
		}
		if configResult.Action != "list" {
			t.Errorf("expected action=list, got %s", configResult.Action)
		}
		if len(configResult.All) == 0 {
			t.Error("expected non-empty config")
		}
	})

	t.Run("get single config", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"key": "active_to_cold_days"}`)
		if err != nil {
			t.Fatal(err)
		}
		var configResult MemoryConfigResult
		if err := json.Unmarshal([]byte(result), &configResult); err != nil {
			t.Fatal(err)
		}
		if configResult.Action != "get" {
			t.Errorf("expected action=get, got %s", configResult.Action)
		}
		if configResult.Value != "30" {
			t.Errorf("expected value=30, got %s", configResult.Value)
		}
	})

	t.Run("set config", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), `{"key": "active_to_cold_days", "value": "14"}`)
		if err != nil {
			t.Fatal(err)
		}
		var configResult MemoryConfigResult
		if err := json.Unmarshal([]byte(result), &configResult); err != nil {
			t.Fatal(err)
		}
		if configResult.Action != "set" {
			t.Errorf("expected action=set, got %s", configResult.Action)
		}
		// Verify it was set
		val, _ := store.GetConfig("active_to_cold_days")
		if val != "14" {
			t.Errorf("expected value=14, got %s", val)
		}
	})
}