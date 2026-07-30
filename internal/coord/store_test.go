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
	t.Cleanup(func() { _ = s.Close() })
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

	_, _, err = s.CreateKey(userID, "provider")
	if err != nil {
		t.Fatalf("CreateKey provider: %v", err)
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

func TestOwnerStats(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(6, "frank")

	ds, err := s.GetOwnerStats(userID)
	if err != nil {
		t.Fatalf("GetOwnerStats: %v", err)
	}
	if ds.TotalRequests != 0 || ds.TotalTokens != 0 || ds.TotalUptimeSec != 0 {
		t.Fatal("expected zero stats")
	}

	if err := s.UpdateOwnerStats(userID, 10, 500, 60); err != nil {
		t.Fatalf("UpdateOwnerStats: %v", err)
	}

	ds, err = s.GetOwnerStats(userID)
	if err != nil {
		t.Fatalf("GetOwnerStats after update: %v", err)
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

	if err := s.UpdateOwnerStats(userID, 5, 200, 30); err != nil {
		t.Fatalf("UpdateOwnerStats second: %v", err)
	}
	ds, _ = s.GetOwnerStats(userID)
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

func TestMachinesInvitesBindings(t *testing.T) {
	s := newTestStore(t)
	ownerID, _ := s.UpsertUser(100, "owner")
	memberID, _ := s.UpsertUser(101, "member")

	_, keyID, err := s.CreateKey(ownerID, "provider")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	m, err := s.UpsertMachineByProviderKey(ownerID, keyID, "home-lab")
	if err != nil {
		t.Fatalf("UpsertMachine: %v", err)
	}
	if m.ID == "" || m.DisplayName != "home-lab" {
		t.Fatalf("unexpected machine: %+v", m)
	}

	m2, err := s.UpsertMachineByProviderKey(ownerID, keyID, "home-lab")
	if err != nil || m2.ID != m.ID {
		t.Fatalf("expected stable machine id, got %v %v", m2, err)
	}

	ok, err := s.CanAccessMachine(ownerID, m.ID)
	if err != nil || !ok {
		t.Fatal("owner should access")
	}
	ok, _ = s.CanAccessMachine(memberID, m.ID)
	if ok {
		t.Fatal("member should not access yet")
	}

	inv, pin, err := s.CreateInvite(m.ID, ownerID, 1, 7, "for friend")
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if pin == "" || inv.ID == 0 {
		t.Fatal("expected pin and invite")
	}

	machineID, _, err := s.RedeemPIN(memberID, pin)
	if err != nil || machineID != m.ID {
		t.Fatalf("RedeemPIN: %v %s", err, machineID)
	}

	ok, _ = s.CanAccessMachine(memberID, m.ID)
	if !ok {
		t.Fatal("member should access after redeem")
	}

	// Exhausted
	_, _, err = s.RedeemPIN(memberID, pin)
	if err != ErrExhausted {
		t.Fatalf("expected exhausted, got %v", err)
	}

	list, err := s.ListAccessibleMachines(memberID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 binding, got %v %v", list, err)
	}

	if err := s.RevokeMemberByOwner(ownerID, m.ID, memberID); err != nil {
		t.Fatalf("RevokeMember: %v", err)
	}
	ok, _ = s.CanAccessMachine(memberID, m.ID)
	if ok {
		t.Fatal("member should not access after revoke")
	}
}

func TestListInviteRedeemers(t *testing.T) {
	s := newTestStore(t)
	ownerID, _ := s.UpsertUser(400, "owner-r")
	memberID, _ := s.UpsertUser(401, "alice")
	_, keyID, _ := s.CreateKey(ownerID, "provider")
	m, _ := s.UpsertMachineByProviderKey(ownerID, keyID, "box")
	_, pin, err := s.CreateInvite(m.ID, ownerID, 3, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	invites, _ := s.ListInvitesByOwner(ownerID)
	if len(invites) != 1 {
		t.Fatalf("expected 1 invite, got %d", len(invites))
	}
	if _, _, err := s.RedeemPIN(memberID, pin); err != nil {
		t.Fatal(err)
	}
	redeemers, err := s.ListInviteRedeemers([]int64{invites[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	got := redeemers[invites[0].ID]
	if len(got) != 1 || got[0] != "alice" {
		t.Fatalf("expected [alice], got %v", got)
	}
}

func TestRegenerateProviderHidesOldMachine(t *testing.T) {
	s := newTestStore(t)
	ownerID, _ := s.UpsertUser(300, "regen-owner")
	memberID, _ := s.UpsertUser(301, "regen-member")

	_, oldKeyID, err := s.CreateKey(ownerID, "provider")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	oldMachine, err := s.UpsertMachineByProviderKey(ownerID, oldKeyID, "box")
	if err != nil {
		t.Fatalf("UpsertMachine: %v", err)
	}
	_, pin, err := s.CreateInvite(oldMachine.ID, ownerID, 1, 7, "")
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, _, err := s.RedeemPIN(memberID, pin); err != nil {
		t.Fatalf("RedeemPIN: %v", err)
	}

	if err := s.RevokeKey(ownerID, oldKeyID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if err := s.RetireMachine(oldMachine.ID); err != nil {
		t.Fatalf("RetireMachine: %v", err)
	}

	_, newKeyID, err := s.CreateKey(ownerID, "provider")
	if err != nil {
		t.Fatalf("CreateKey new: %v", err)
	}
	newMachine, err := s.CreateMachineOnKeyRegen(ownerID, newKeyID, "box")
	if err != nil {
		t.Fatalf("CreateMachineOnKeyRegen: %v", err)
	}
	if newMachine.ID == oldMachine.ID {
		t.Fatal("expected new machine id after regen")
	}

	owned, err := s.ListMachinesByOwner(ownerID)
	if err != nil {
		t.Fatalf("ListMachinesByOwner: %v", err)
	}
	if len(owned) != 1 || owned[0].ID != newMachine.ID {
		t.Fatalf("expected only new machine, got %+v", owned)
	}

	ok, _ := s.CanAccessMachine(ownerID, oldMachine.ID)
	if ok {
		t.Fatal("owner should not access retired machine")
	}
	ok, _ = s.CanAccessMachine(memberID, oldMachine.ID)
	if ok {
		t.Fatal("member should not access retired machine")
	}

	memberList, err := s.ListAccessibleMachines(memberID)
	if err != nil {
		t.Fatalf("ListAccessibleMachines: %v", err)
	}
	if len(memberList) != 0 {
		t.Fatalf("member should have no bindings after retire, got %+v", memberList)
	}
}

func TestScopeDonorNormalizedToProvider(t *testing.T) {
	s := newTestStore(t)
	userID, _ := s.UpsertUser(200, "norm")
	raw, _, err := s.CreateKey(userID, "donor")
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.FindKeyByHash(hashKey(raw))
	if err != nil || k == nil {
		t.Fatal("key not found")
	}
	if k.Scope != "provider" {
		t.Fatalf("expected provider, got %s", k.Scope)
	}
}
