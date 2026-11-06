// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/zm2231/skool-cli/internal/store"
)

func TestDecodeRosterBillingMember(t *testing.T) {
	rec := json.RawMessage(`{
		"name":"sam-narang-3428","firstName":"Sam","lastName":"Narang",
		"member":{
			"role":"member","approvedAt":"2026-08-13T21:34:52Z","inviteEmail":null,
			"metadata":{
				"mbme":"coolbudy.samdarsh@gmail.com",
				"mbscpe":1787261691,
				"mmbp":"{\"currency\":\"usd\",\"amount\":9700,\"model\":\"subscription\"}",
				"msbs":"sub_1U46H1K2xk1aF7GmYSncNrcw",
				"attrComp":"direct","isFreeTrial":1
			}
		}
	}`)
	rows := decodeRoster([]json.RawMessage{rec})
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Email != "coolbudy.samdarsh@gmail.com" || r.EmailSource != "billing" {
		t.Fatalf("mbme should decode to a billing email, got %q/%q", r.Email, r.EmailSource)
	}
	if r.PriceUSDMo != 97.0 || r.Model != "subscription" {
		t.Fatalf("mmbp should decode to $97/subscription, got %v/%q", r.PriceUSDMo, r.Model)
	}
	if r.PeriodEnd != "2026-08-20T21:34:51Z" {
		t.Fatalf("mbscpe should decode to a period-end timestamp, got %q", r.PeriodEnd)
	}
	if !r.IsTrial || r.StripeSub != "sub_1U46H1K2xk1aF7GmYSncNrcw" || r.JoinSource != "direct" {
		t.Fatalf("unexpected trial/stripe/source: %+v", r)
	}
}

func TestDecodeRosterInviteFallback(t *testing.T) {
	rec := json.RawMessage(`{
		"name":"invited-user","firstName":"In","lastName":"Vited",
		"member":{"role":"member","inviteEmail":"invited@example.com","metadata":{}}
	}`)
	r := decodeRoster([]json.RawMessage{rec})[0]
	if r.Email != "invited@example.com" || r.EmailSource != "invite" {
		t.Fatalf("no billing email should fall back to inviteEmail, got %q/%q", r.Email, r.EmailSource)
	}
	if r.PriceUSDMo != 0 || r.PeriodEnd != "" {
		t.Fatalf("free/invited member should have no price or period-end, got %v/%q", r.PriceUSDMo, r.PeriodEnd)
	}
}

func TestFilterRosterExpiring(t *testing.T) {
	rows := []rosterRow{
		{Handle: "soon", MembershipState: "active", PeriodEnd: "2020-01-01T00:00:00Z"},
		{Handle: "later", MembershipState: "active", PeriodEnd: "2999-01-01T00:00:00Z"},
		{Handle: "none", MembershipState: "active", PeriodEnd: ""},
	}
	all := []string{"active", "cancelling", "churned", "banned"}
	got := filterRoster(append([]rosterRow(nil), rows...), all, false, 7)
	if len(got) != 1 || got[0].Handle != "soon" {
		t.Fatalf("expiring filter should keep only past/near period-ends, got %+v", got)
	}
}

func TestFilterRosterByStatus(t *testing.T) {
	rows := []rosterRow{
		{Handle: "a", MembershipState: "active"},
		{Handle: "c", MembershipState: "churned"},
		{Handle: "b", MembershipState: "banned"},
	}
	got := filterRoster(append([]rosterRow(nil), rows...), []string{"churned", "banned"}, false, 0)
	if len(got) != 2 {
		t.Fatalf("status filter should keep churned+banned only, got %+v", got)
	}
	for _, r := range got {
		if r.MembershipState == "active" {
			t.Fatalf("active member leaked through churned/banned filter")
		}
	}
}

func TestParseStatuses(t *testing.T) {
	all, err := parseStatuses("all")
	if err != nil || len(all) != 5 {
		t.Fatalf("all should expand to 5 states (4 tabs + vanished), got %v err=%v", all, err)
	}
	got, err := parseStatuses("churned, banned")
	if err != nil || len(got) != 2 || got[0] != "churned" || got[1] != "banned" {
		t.Fatalf("comma list parse failed: %v err=%v", got, err)
	}
	if _, err := parseStatuses("bogus"); err == nil {
		t.Fatal("unknown status should error")
	}
}

func TestDecodeRosterChurned(t *testing.T) {
	rec := json.RawMessage(`{
		"name":"gone-user","firstName":"Gone","lastName":"User","mbState":"churned",
		"member":{"role":"member","churned":"2026-04-13T23:32:17Z","metadata":{"mbme":"gone@example.com"}}
	}`)
	r := decodeRoster([]json.RawMessage{rec})[0]
	if r.MembershipState != "churned" || r.IsActive {
		t.Fatalf("churned member should be inactive, got state=%q active=%v", r.MembershipState, r.IsActive)
	}
	if r.ChurnedAt != "2026-04-13T23:32:17Z" {
		t.Fatalf("churned_at should carry member.churned, got %q", r.ChurnedAt)
	}
}

func TestDecodeRosterVanished(t *testing.T) {
	rec := json.RawMessage(`{
		"id":"9d928a8f049841deb5c66a613e6e3ece","name":"gone-entirely-1","firstName":"Gone","lastName":"Entirely",
		"mbState":"vanished","vanished_at":"2026-08-14T00:00:00Z",
		"member":{"role":"member","metadata":{"mbme":"stillknown@example.com"}}
	}`)
	r := decodeRoster([]json.RawMessage{rec})[0]
	if r.MembershipState != "vanished" || r.IsActive {
		t.Fatalf("vanished member should be inactive, got state=%q active=%v", r.MembershipState, r.IsActive)
	}
	if r.ID != "9d928a8f049841deb5c66a613e6e3ece" {
		t.Fatalf("roster row must carry the Skool id join key, got %q", r.ID)
	}
	if r.Email != "stillknown@example.com" || r.EmailSource != "billing" {
		t.Fatalf("vanished row should retain last-known email, got %q/%q", r.Email, r.EmailSource)
	}
	if r.VanishedAt != "2026-08-14T00:00:00Z" {
		t.Fatalf("vanished_at should decode, got %q", r.VanishedAt)
	}
}

func seedMember(t *testing.T, db *store.Store, id, state string) {
	t.Helper()
	rec := json.RawMessage(`{"id":"` + id + `","name":"` + id + `","mbState":"` + state + `","member":{"role":"member","metadata":{"mbme":"` + id + `@example.com"}}}`)
	if _, _, err := db.UpsertBatch("members", []json.RawMessage{rec}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func memberState(t *testing.T, db *store.Store, id string) string {
	t.Helper()
	raw, err := db.Get("members", id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	var obj struct {
		MbState string `json:"mbState"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal %s: %v", id, err)
	}
	return obj.MbState
}

func TestReconcileVanishedMarksAbsentAboveFloor(t *testing.T) {
	db, err := store.OpenWithContext(context.Background(), filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ids := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		id := "m" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		ids = append(ids, id)
		seedMember(t, db, id, "active")
	}
	seen := map[string]struct{}{}
	for _, id := range ids[:49] {
		seen[id] = struct{}{}
	}
	if err := reconcileVanished(db, "", seen); err != nil {
		t.Fatal(err)
	}
	if s := memberState(t, db, ids[49]); s != stateVanished {
		t.Fatalf("member absent from a trusted sweep should be vanished, got %q", s)
	}
	if s := memberState(t, db, ids[0]); s != "active" {
		t.Fatalf("member present in sweep must stay active, got %q", s)
	}
}

func TestReconcileVanishedSkipsBelowFloor(t *testing.T) {
	db, err := store.OpenWithContext(context.Background(), filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ids := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		id := "z" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		ids = append(ids, id)
		seedMember(t, db, id, "active")
	}
	seen := map[string]struct{}{ids[0]: {}, ids[1]: {}, ids[2]: {}}
	if err := reconcileVanished(db, "", seen); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if s := memberState(t, db, id); s == stateVanished {
			t.Fatalf("a 3-member sweep is below the floor; nobody should be vanished, but %s was", id)
		}
	}
}

func TestStatePriorityActiveOutranksTombstones(t *testing.T) {
	if statePriority("active") <= statePriority("banned") {
		t.Fatal("active must outrank banned so a live member is not clobbered by a banned tombstone")
	}
	if statePriority("active") <= statePriority("churned") {
		t.Fatal("active must outrank churned")
	}
	if statePriority("cancelling") <= statePriority("churned") {
		t.Fatal("cancelling is still a paying member and must outrank churned")
	}
	if statePriority("banned") <= statePriority("churned") {
		t.Fatal("a deliberate ban must outrank a churn when Skool dual-lists a member")
	}
	if statePriority("banned") <= statePriority(stateVanished) {
		t.Fatal("a real banned record must outrank a synthetic vanished tombstone")
	}
}

func TestTagMembershipState(t *testing.T) {
	tagged := tagMembershipState(json.RawMessage(`{"id":"x","member":{}}`), "banned")
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(tagged, &obj); err != nil {
		t.Fatalf("tagged record must stay valid JSON: %v", err)
	}
	var state string
	if json.Unmarshal(obj["mbState"], &state) != nil || state != "banned" {
		t.Fatalf("mbState should be set to banned, got %s", obj["mbState"])
	}
	if _, ok := obj["id"]; !ok {
		t.Fatal("tagging must preserve the original id")
	}
}
