package server

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func orderMerchantFixture(t *testing.T, srv *httptest.Server, token, csrf string) string {
	t.Helper()
	status, out, _ := apiRequest(t, srv, "POST", "/api/v1/merchants", token, csrf, map[string]string{"legal_name": "Synthetic merchant", "contact_name": "Testing", "contact_email": "synthetic@example.invalid"})
	if status != 201 {
		t.Fatalf("create merchant returned %d", status)
	}
	return out["merchant"].(map[string]any)["id"].(string)
}
func orderResourceFixtures(t *testing.T, s *Server, u User, mid string) (inst, va, target string) {
	t.Helper()
	ctx := context.Background()
	inst, va, target = newID(), newID(), newID()
	enrollment, targetID := newID(), newID()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO provider_instances(id,provider_code,instance_code,environment,created_by) VALUES($1,'test',$2,'sandbox',$3)`, inst, newID(), u.ID)
	exec(`INSERT INTO provider_bindings(id,merchant_id,instance_id,kind,label,account_scope,external_key,masked_external_id,details_enc,profile_version,asset,created_by) VALUES($1,$2,$3,'enrollment','fixture','s','e','masked','fixture',1,'',$4)`, enrollment, mid, inst, u.ID)
	exec(`INSERT INTO provider_bindings(id,merchant_id,instance_id,kind,label,account_scope,external_key,masked_external_id,details_enc,profile_version,asset,parent_binding_id,created_by) VALUES($1,$2,$3,'virtual_account','fixture','s','v','masked','fixture',1,'USD',$4,$5)`, va, mid, inst, enrollment, u.ID)
	exec(`INSERT INTO payment_targets(id,merchant_id,kind,created_by) VALUES($1,$2,'chain',$3)`, targetID, mid, u.ID)
	exec(`INSERT INTO payment_target_versions(id,merchant_id,target_id,version,kind,label,asset,network,masked_destination,details_enc,created_by,request_id,request_digest) VALUES($1,$2,$3,1,'chain','fixture','USDT','testnet','masked','fixture',$4,$1,decode(repeat('00',32),'hex'))`, target, mid, targetID, u.ID)
	return inst, va, target
}
func TestOrderConcurrentCreateAndScopedReplay(t *testing.T) {
	s, srv := testServer(t)
	u := seedUser(t, s, []string{"operator"}, "")
	token, csrf := authenticated(t, s, u)
	mid := orderMerchantFixture(t, srv, token, csrf)
	base := "/api/v1/merchants/" + mid + "/orders"
	in := orderInput{FiatAsset: "USD", RequestID: newID(), Direction: "fiat_to_crypto", ProfileVersion: 1, SourceAmount: "123456789012345678901234567890123456.78", Instruction: "Exact original instruction\nLine two"}
	type result struct {
		status int
		out    map[string]any
	}
	results := make(chan result, 6)
	var wg sync.WaitGroup
	for range cap(results) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, r, _ := apiRequest(t, srv, "POST", base, token, csrf, in)
			results <- result{n, r}
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	created := 0
	for r := range results {
		if r.status != 201 && r.status != 200 {
			t.Fatalf("create returned %d %v", r.status, r.out)
		}
		if r.status == 201 {
			created++
		}
		o := r.out["order"].(map[string]any)
		if id == "" {
			id = o["id"].(string)
		}
		if o["id"] != id || o["source_amount"] != in.SourceAmount || o["status"] != "blocked" {
			t.Fatal("inconsistent create replay")
		}
	}
	if created != 1 {
		t.Fatalf("%d created", created)
	}
	in.Instruction += " amended"
	status, _, _ := apiRequest(t, srv, "POST", base, token, csrf, in)
	if status != 409 {
		t.Fatal("changed payload reused key", status)
	}
	other := seedUser(t, s, []string{"operator"}, "")
	ot, oc := authenticated(t, s, other)
	in.Instruction = strings.TrimSuffix(in.Instruction, " amended")
	status, _, _ = apiRequest(t, srv, "POST", base, ot, oc, in)
	if status != 409 {
		t.Fatal("cross-operator key replay", status)
	}
	status, out, _ := apiRequest(t, srv, "GET", base, token, csrf, nil)
	if status != 200 || len(out["items"].([]any)) != 1 {
		t.Fatal("duplicate list entries")
	}
	if _, ok := out["items"].([]any)[0].(map[string]any)["instruction"]; ok {
		t.Fatal("list leaked raw instruction")
	}
	mid2 := orderMerchantFixture(t, srv, token, csrf)
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/merchants/"+mid2+"/orders/"+id, token, csrf, nil)
	if status != 404 {
		t.Fatal("cross-merchant read", status)
	}
	root := seedUser(t, s, []string{"root"}, "")
	rt, rc := authenticated(t, s, root)
	status, _, _ = apiRequest(t, srv, "GET", base, rt, rc, nil)
	if status != 403 {
		t.Fatal("root read merchant orders", status)
	}
	status, _, _ = apiRequest(t, srv, "POST", base, token, "", in)
	if status != 403 {
		t.Fatal("CSRF bypass", status)
	}
	in.RequestID = newID()
	in.SourceAmount = "0.001"
	status, _, _ = apiRequest(t, srv, "POST", base, token, csrf, in)
	if status != 400 {
		t.Fatal("rounded invalid source", status)
	}
	in.RequestID = newID()
	in.Direction = "crypto_to_fiat"
	in.SourceAmount = "0.000001"
	status, out, _ = apiRequest(t, srv, "POST", base, token, csrf, in)
	if status != 201 {
		t.Fatal("reverse direction", status)
	}
	rev := out["order"].(map[string]any)
	if rev["source_asset"] != "USDT" || rev["destination_asset"] != "USD" || !containsOrderAction(rev, "bank_payout") {
		t.Fatal("incorrect reverse plan")
	}
}
func containsOrderAction(order map[string]any, action string) bool {
	for _, v := range order["steps"].([]any) {
		if v.(map[string]any)["action"] == action {
			return true
		}
	}
	return false
}
func TestOrderVersionPinningAndEvidenceIsolation(t *testing.T) {
	s, srv := testServer(t)
	u := seedUser(t, s, []string{"operator"}, "")
	token, csrf := authenticated(t, s, u)
	mid := orderMerchantFixture(t, srv, token, csrf)
	other := orderMerchantFixture(t, srv, token, csrf)
	base := "/api/v1/merchants/" + mid + "/orders"
	inst, a, target := orderResourceFixtures(t, s, u, mid)
	in := orderInput{FiatAsset: "USD", RequestID: newID(), Direction: "fiat_to_crypto", ProfileVersion: 1, SourceAmount: "45.67", Instruction: "  retain exact original\n", TargetVersionID: &target, ChannelBindingID: &a, Quote: &quoteInput{Rate: "1.0001", DestinationAmount: "45.674567", ExpiresAt: time.Now().Add(-time.Minute), Evidence: "synthetic original quote evidence"}}
	status, out, _ := apiRequest(t, srv, "POST", base, token, csrf, in)
	if status != 201 {
		t.Fatalf("create %d %v", status, out)
	}
	first := out["order"].(map[string]any)
	id := first["id"].(string)
	if first["instruction"] != in.Instruction || first["quote"].(map[string]any)["expired"] != true || first["hold_reason"] != "报价已过期" {
		t.Fatalf("missing original facts or stale quote: %v", first)
	}
	var rawInstruction, rawQuote []byte
	if err := s.Pool.QueryRow(context.Background(), `SELECT instruction_enc,evidence_enc FROM order_versions JOIN order_quote_versions USING(order_id,version) WHERE order_id=$1`, id).Scan(&rawInstruction, &rawQuote); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawInstruction, []byte(in.Instruction)) || bytes.Contains(rawQuote, []byte(in.Quote.Evidence)) {
		t.Fatal("raw evidence stored as plaintext")
	}
	_, b, _ := orderResourceFixtures(t, s, u, mid)
	_, foreignBinding, foreignTarget := orderResourceFixtures(t, s, u, other)
	revise := in
	revise.RequestID = newID()
	revise.ExpectedVersion = 1
	revise.Instruction = "second instruction"
	revise.ChannelBindingID = &b
	path := base + "/" + id
	erroneous := revise
	erroneous.TargetVersionID = &foreignTarget
	status, _, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, erroneous)
	if status != 400 {
		t.Fatal("foreign target accepted", status)
	}
	erroneous = revise
	erroneous.ChannelBindingID = &foreignBinding
	status, _, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, erroneous)
	if status != 400 {
		t.Fatal("foreign VA accepted", status)
	}
	status, out, _ = apiRequest(t, srv, "GET", path, token, csrf, nil)
	if status != 200 || out["order"].(map[string]any)["current_version"] != float64(1) {
		t.Fatal("partial revision persisted")
	}
	status, out, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, revise)
	if status != 201 || out["order"].(map[string]any)["version"] != float64(2) {
		t.Fatal("revision not saved", status, out)
	}
	status, _, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, revise)
	if status != 200 {
		t.Fatal("identical revision replay", status)
	}
	stale := revise
	stale.RequestID = newID()
	status, _, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, stale)
	if status != 409 {
		t.Fatal("stale write", status)
	}
	// Update the merchant and append another target version. Old orders must render
	// their pinned names and masked identifiers even when the resource list changes.
	status, _, _ = apiRequest(t, srv, "PATCH", "/api/v1/merchants/"+mid, token, csrf, map[string]any{"row_version": 1, "service_status": "active", "legal_name": "Updated name", "contact_name": "Test contact", "contact_email": "new@example.invalid"})
	if status != 200 {
		t.Fatal("merchant update", status)
	}
	_, err := s.Pool.Exec(context.Background(), `INSERT INTO payment_target_versions(id,merchant_id,target_id,version,kind,label,asset,network,masked_destination,details_enc,created_by,request_id,request_digest) SELECT $1,merchant_id,target_id,version+1,kind,'new target label',asset,network,'new masked destination',details_enc,created_by,$2,request_digest FROM payment_target_versions WHERE id=$3`, newID(), newID(), target)
	if err != nil {
		t.Fatal(err)
	}
	fresh := revise
	fresh.ExpectedVersion = 2
	fresh.ProfileVersion = 2
	fresh.RequestID = newID()
	status, out, _ = apiRequest(t, srv, "POST", path+"/versions", token, csrf, fresh)
	if status != 409 || out["error"].(map[string]any)["code"] != "target_version_conflict" {
		t.Fatal("superseded target accepted on fresh revision", status, out)
	}
	fresh.ExpectedVersion = 0
	status, out, _ = apiRequest(t, srv, "POST", base, token, csrf, fresh)
	if status != 409 || out["error"].(map[string]any)["code"] != "target_version_conflict" {
		t.Fatal("superseded target accepted on fresh create", status, out)
	}
	status, out, _ = apiRequest(t, srv, "POST", base, token, csrf, in)
	if status != 200 || out["order"].(map[string]any)["version"] != float64(1) {
		t.Fatal("original replay rejected after target changed", status, out)
	}
	status, out, _ = apiRequest(t, srv, "GET", path+"?version=1", token, csrf, nil)
	if status != 200 {
		t.Fatal(status)
	}
	historical := out["order"].(map[string]any)
	if historical["channel_binding_id"] != a || historical["instruction"] != in.Instruction || historical["current_version"] != float64(2) {
		t.Fatal("historical pin changed")
	}
	if historical["pinned_target"].(map[string]any)["masked_destination"] != "masked" || historical["pinned_profile"].(map[string]any)["legal_name"] != "Synthetic merchant" || historical["pinned_binding"].(map[string]any)["instance_id"] != inst {
		t.Fatal("historical resources resolved from latest state")
	}
	for _, v := range historical["steps"].([]any) {
		step := v.(map[string]any)
		if step["status"] != "planned" {
			t.Fatal("unexpected executable step")
		}
		if step["action"] == "verify_fiat_receipt" && (step["binding_id"] != a || step["instance_id"] != inst) {
			t.Fatal("receipt routed via another instance")
		}
		if step["action"] != "verify_fiat_receipt" && step["instance_id"] != nil {
			t.Fatal("unverified payout route inferred")
		}
	}
	if _, err := s.Pool.Exec(context.Background(), `UPDATE order_versions SET instruction_enc='rewrite' WHERE order_id=$1`, id); err == nil {
		t.Fatal("historical instruction mutable")
	}
}

func TestOrderCurrencySnapshotSurvivesDefaultChange(t *testing.T) {
	s, oldEndpoint := testServer(t)
	oldEndpoint.Close()
	hk := *s
	hk.IntakeFiatAsset = "HKD"
	sandbox := httptest.NewServer(hk.Handler())
	defer sandbox.Close()
	usd := httptest.NewServer(s.Handler())
	defer usd.Close()
	u := seedUser(t, s, []string{"operator"}, "")
	token, csrf := authenticated(t, s, u)
	mid := orderMerchantFixture(t, sandbox, token, csrf)
	base := "/api/v1/merchants/" + mid + "/orders"
	in := orderInput{FiatAsset: "HKD", Direction: "fiat_to_crypto", SourceAmount: "61.80", Instruction: "HKD only", ProfileVersion: 1, RequestID: newID()}
	status, out, _ := apiRequest(t, sandbox, "POST", base, token, csrf, in)
	if status != 201 {
		t.Fatal(status, out)
	}
	first := out["order"].(map[string]any)
	if first["source_asset"] != "HKD" {
		t.Fatal("sandbox stored wrong fiat")
	}
	id := first["id"].(string)
	status, out, _ = apiRequest(t, usd, "POST", base, token, csrf, in)
	if status != 200 || out["order"].(map[string]any)["source_asset"] != "HKD" {
		t.Fatal("old request reinterpreted after config change")
	}
	in.RequestID = newID()
	status, _, _ = apiRequest(t, usd, "POST", base, token, csrf, in)
	if status != 409 {
		t.Fatal("stale form currency accepted", status)
	}
	in.ExpectedVersion = 1
	in.Instruction = "Revise original HKD"
	status, out, _ = apiRequest(t, usd, "POST", base+"/"+id+"/versions", token, csrf, in)
	if status != 201 || out["order"].(map[string]any)["source_asset"] != "HKD" {
		t.Fatal("old order lost currency", status, out)
	}
	in.FiatAsset = "USD"
	in.RequestID = newID()
	in.ExpectedVersion = 2
	status, _, _ = apiRequest(t, usd, "POST", base+"/"+id+"/versions", token, csrf, in)
	if status != 400 {
		t.Fatal("changed order currency accepted", status)
	}
	in.Direction = "crypto_to_fiat"
	in.FiatAsset = "HKD"
	in.ExpectedVersion = 0
	in.RequestID = newID()
	status, out, _ = apiRequest(t, sandbox, "POST", base, token, csrf, in)
	if status != 201 || out["order"].(map[string]any)["destination_asset"] != "HKD" {
		t.Fatal("reverse order lost sandbox destination", status, out)
	}
}
