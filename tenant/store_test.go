package tenant

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ============================================================
// Key CRUD
// ============================================================

func TestCreateKey(t *testing.T) {
	s := newTestStore(t)
	key, err := s.CreateKey("test-app", WithQPM(100), WithModels([]string{"gpt-4"}))
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if key.Name != "test-app" {
		t.Errorf("name = %q", key.Name)
	}
	if key.QPM != 100 {
		t.Errorf("qpm = %d", key.QPM)
	}
	if len(key.AllowedModels) != 1 || key.AllowedModels[0] != "gpt-4" {
		t.Errorf("models = %v", key.AllowedModels)
	}
	if !key.Enabled {
		t.Error("should be enabled by default")
	}
	if key.Key == "" || len(key.Key) < 10 {
		t.Error("key should be generated")
	}
	if key.ID == "" {
		t.Error("id should be generated")
	}
}

func TestGetKeyByToken(t *testing.T) {
	s := newTestStore(t)
	created, _ := s.CreateKey("lookup-test")

	found, ok := s.GetKeyByToken(created.Key)
	if !ok {
		t.Fatal("key not found")
	}
	if found.ID != created.ID {
		t.Errorf("id mismatch: %s vs %s", found.ID, created.ID)
	}

	_, ok = s.GetKeyByToken("nonexistent")
	if ok {
		t.Error("should not find nonexistent key")
	}
}

func TestGetKeyByID(t *testing.T) {
	s := newTestStore(t)
	created, _ := s.CreateKey("id-test")

	found, err := s.GetKeyByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Name != "id-test" {
		t.Error("name mismatch")
	}

	_, err = s.GetKeyByID("nonexistent")
	if err == nil {
		t.Error("should error on missing ID")
	}
}

func TestListKeys(t *testing.T) {
	s := newTestStore(t)
	s.CreateKey("a")
	s.CreateKey("b")
	s.CreateKey("c")

	keys := s.ListKeys()
	if len(keys) != 3 {
		t.Errorf("expected 3, got %d", len(keys))
	}
}

func TestUpdateKey(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("original")

	updated, err := s.UpdateKey(key.ID, WithName("renamed"), WithQPM(50), WithEnabled(false))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "renamed" {
		t.Errorf("name = %q", updated.Name)
	}
	if updated.QPM != 50 {
		t.Error("qpm should be updated")
	}
	if updated.Enabled {
		t.Error("should be disabled")
	}

	// Verify cache is updated
	found, ok := s.GetKeyByToken(key.Key)
	if !ok {
		t.Fatal("key should still be in cache")
	}
	if found.Name != "renamed" {
		t.Error("cache not updated")
	}

	_, err = s.UpdateKey("nonexistent", WithName("x"))
	if err == nil {
		t.Error("should error on missing ID")
	}
}

func TestDeleteKey(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("to-delete")

	if err := s.DeleteKey(key.ID); err != nil {
		t.Fatal(err)
	}

	_, ok := s.GetKeyByToken(key.Key)
	if ok {
		t.Error("key should be removed from cache")
	}

	if err := s.DeleteKey(key.ID); err == nil {
		t.Error("double delete should error")
	}
}

// ============================================================
// Usage Recording + Query
// ============================================================

func TestRecordAndQueryUsage(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("usage-test")

	// Record some usage
	for i := 0; i < 5; i++ {
		s.RecordUsage(&UsageRecord{
			KeyID:        key.ID,
			Model:        "gpt-4",
			Provider:     "openai",
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
			Duration:     200.0,
			CreatedAt:    time.Now().UTC(),
		})
	}

	summaries, err := s.QueryUsage(UsageQuery{KeyID: key.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TotalRequests != 5 {
		t.Errorf("requests = %d", summaries[0].TotalRequests)
	}
	if summaries[0].InputTokens != 500 {
		t.Errorf("input tokens = %d", summaries[0].InputTokens)
	}
	if summaries[0].TotalTokens != 750 {
		t.Errorf("total tokens = %d", summaries[0].TotalTokens)
	}
}

func TestQueryUsage_TimeFilter(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("time-test")

	now := time.Now().UTC()
	s.RecordUsage(&UsageRecord{KeyID: key.ID, TotalTokens: 100, CreatedAt: now.Add(-2 * time.Hour)})
	s.RecordUsage(&UsageRecord{KeyID: key.ID, TotalTokens: 200, CreatedAt: now})

	summaries, _ := s.QueryUsage(UsageQuery{KeyID: key.ID, From: now.Add(-1 * time.Hour)})
	if len(summaries) != 1 || summaries[0].TotalTokens != 200 {
		t.Errorf("time filter failed: %+v", summaries)
	}
}

func TestCountRecentRequests(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("count-test")

	for i := 0; i < 3; i++ {
		s.RecordUsage(&UsageRecord{KeyID: key.ID, CreatedAt: time.Now().UTC()})
	}

	count, err := s.CountRecentRequests(key.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("count = %d", count)
	}
}

func TestCountRecentTokens(t *testing.T) {
	s := newTestStore(t)
	key, _ := s.CreateKey("tokens-test")

	s.RecordUsage(&UsageRecord{KeyID: key.ID, TotalTokens: 500, CreatedAt: time.Now().UTC()})
	s.RecordUsage(&UsageRecord{KeyID: key.ID, TotalTokens: 300, CreatedAt: time.Now().UTC()})

	total, err := s.CountRecentTokens(key.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if total != 800 {
		t.Errorf("total = %d", total)
	}
}

// ============================================================
// Cache persistence
// ============================================================

func TestCachePersistsAcrossReload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	// Create store and add a key
	s1, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := s1.CreateKey("persist-test")
	s1.Close()

	// Reopen and check cache
	s2, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	found, ok := s2.GetKeyByToken(key.Key)
	if !ok {
		t.Fatal("key should persist across reload")
	}
	if found.Name != "persist-test" {
		t.Error("name mismatch after reload")
	}
}

// ============================================================
// Rate Limiter
// ============================================================

func TestRateLimiter_QPM(t *testing.T) {
	rl := NewRateLimiter()

	// Allow 3 QPM
	for i := 0; i < 3; i++ {
		if !rl.AllowRequest("key1", 3) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th should be denied
	if rl.AllowRequest("key1", 3) {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiter_QPM_Unlimited(t *testing.T) {
	rl := NewRateLimiter()
	for i := 0; i < 100; i++ {
		if !rl.AllowRequest("key1", 0) {
			t.Fatal("unlimited QPM should always allow")
		}
	}
}

func TestRateLimiter_TPM(t *testing.T) {
	rl := NewRateLimiter()

	if !rl.AllowTokens("key1", 1000, 500) {
		t.Error("500 tokens should be allowed under 1000 TPM")
	}
	if !rl.AllowTokens("key1", 1000, 400) {
		t.Error("900 cumulative should be allowed")
	}
	if rl.AllowTokens("key1", 1000, 200) {
		t.Error("1100 cumulative should be denied")
	}
}

func TestRateLimiter_RetryAfter(t *testing.T) {
	rl := NewRateLimiter()
	rl.AllowRequest("key1", 1)

	after := rl.RetryAfter("key1")
	if after <= 0 || after > 61 {
		t.Errorf("retry after = %d", after)
	}
}

func TestRateLimiter_IsolatesKeys(t *testing.T) {
	rl := NewRateLimiter()
	rl.AllowRequest("key1", 1)

	// key2 should be independent
	if !rl.AllowRequest("key2", 1) {
		t.Error("key2 should be independent from key1")
	}
}

func TestRateLimiter_RecordTokens(t *testing.T) {
	rl := NewRateLimiter()
	rl.RecordTokens("key1", 500)

	if rl.AllowTokens("key1", 600, 200) {
		t.Error("700 total should exceed 600 TPM")
	}
}
