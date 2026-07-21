package coord

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertUser(t *testing.T) {
	s := newTestStore(t)

	id1, err := s.UpsertUser(12345, "testuser")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero user id")
	}

	// Re-upsert should return same ID.
	id2, err := s.UpsertUser(12345, "testuser_updated")
	if err != nil {
		t.Fatalf("UpsertUser second: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same user ID: %d != %d", id1, id2)
	}

	login, err := s.GetUserByID(id1)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if login != "testuser_updated" {
		t.Fatalf("expected login 'testuser_updated', got %q", login)
	}
}

func TestCreateAndListKeys(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(1, "alice")

	raw1, keyID1, err := s.CreateKey(userID, "consumer")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if raw1 == "" || keyID1 == 0 {
		t.Fatal("expected non-empty key and non-zero id")
	}
	if len(raw1) != 36 { // "inf_" + 32 hex
		t.Fatalf("expected key length 36, got %d", len(raw1))
	}

	_, _, err = s.CreateKey(userID, "donor")
	if err != nil {
		t.Fatalf("CreateKey donor: %v", err)
	}

	keys, err := s.ListKeys(userID)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestFindKeyByHash(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(2, "bob")

	raw, _, err := s.CreateKey(userID, "consumer")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	hash := hashKey(raw)
	found, err := s.FindKeyByHash(hash)
	if err != nil {
		t.Fatalf("FindKeyByHash: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find key")
	}
	if found.UserID != userID {
		t.Fatalf("expected userID %d, got %d", userID, found.UserID)
	}
	if found.Scope != "consumer" {
		t.Fatalf("expected scope consumer, got %s", found.Scope)
	}
}

func TestFindKeyByHashNotFound(t *testing.T) {
	s := newTestStore(t)
	found, err := s.FindKeyByHash("deadbeef")
	if err != nil {
		t.Fatalf("FindKeyByHash: %v", err)
	}
	if found != nil {
		t.Fatal("expected nil for unknown hash")
	}
}

func TestRevokeKey(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(3, "carol")

	raw, keyID, err := s.CreateKey(userID, "consumer")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	if err := s.RevokeKey(userID, keyID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}

	// Should not be findable after revoke.
	hash := hashKey(raw)
	found, _ := s.FindKeyByHash(hash)
	if found != nil {
		t.Fatal("expected nil after revoke")
	}

	// ListKeys should not include revoked.
	keys, _ := s.ListKeys(userID)
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys after revoke, got %d", len(keys))
	}
}

func TestRevokeKeyWrongUser(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(4, "dave")
	_, _ = s.UpsertUser(5, "eve")

	_, keyID, _ := s.CreateKey(userID, "consumer")

	// Eve cannot revoke Dave's key.
	err := s.RevokeKey(5, keyID)
	if err == nil {
		t.Fatal("expected error revoking another user's key")
	}
}

func TestDonorStats(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(6, "frank")

	// Fresh stats should be zero.
	ds, err := s.GetDonorStats(userID)
	if err != nil {
		t.Fatalf("GetDonorStats: %v", err)
	}
	if ds.TotalRequests != 0 || ds.TotalTokens != 0 || ds.TotalUptimeSec != 0 {
		t.Fatal("expected zero stats")
	}

	if err := s.UpdateDonorStats(userID, 10, 500, 60); err != nil {
		t.Fatalf("UpdateDonorStats: %v", err)
	}

	ds, err = s.GetDonorStats(userID)
	if err != nil {
		t.Fatalf("GetDonorStats after update: %v", err)
	}
	if ds.TotalRequests != 10 {
		t.Fatalf("expected 10 requests, got %d", ds.TotalRequests)
	}
	if ds.TotalTokens != 500 {
		t.Fatalf("expected 500 tokens, got %d", ds.TotalTokens)
	}
	if ds.TotalUptimeSec != 60 {
		t.Fatalf("expected 60 uptime, got %d", ds.TotalUptimeSec)
	}

	// Second update accumulates.
	if err := s.UpdateDonorStats(userID, 5, 200, 30); err != nil {
		t.Fatalf("UpdateDonorStats second: %v", err)
	}
	ds, _ = s.GetDonorStats(userID)
	if ds.TotalRequests != 15 {
		t.Fatalf("expected accumulated 15 requests, got %d", ds.TotalRequests)
	}
}

func TestSessions(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(7, "grace")

	token, err := s.CreateSession(userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty session token")
	}

	uid, err := s.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if uid != userID {
		t.Fatalf("expected userID %d, got %d", userID, uid)
	}

	// Invalid token.
	uid, err = s.ValidateSession("invalid")
	if err != nil {
		t.Fatalf("ValidateSession invalid: %v", err)
	}
	if uid != 0 {
		t.Fatal("expected 0 for invalid token")
	}

	// Delete session.
	if err := s.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	uid, _ = s.ValidateSession(token)
	if uid != 0 {
		t.Fatal("expected 0 after delete")
	}
}
