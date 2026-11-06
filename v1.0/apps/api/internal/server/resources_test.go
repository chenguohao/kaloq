package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func resourceTestMerchant(t *testing.T, srv *httptest.Server, token, csrf, label string) string {
	t.Helper()
	status, out, _ := apiRequest(t, srv, "POST", "/api/v1/merchants", token, csrf, map[string]any{"legal_name": label, "contact_name": "test", "contact_email": "test@example.invalid"})
	if status != 201 {
		t.Fatalf("create merchant HTTP %d", status)
	}
	return out["merchant"].(map[string]any)["id"].(string)
}

func TestResourcesBindingIdentityAndIsolation(t *testing.T) {
	s, srv := testServer(t)
	operator := seedUser(t, s, []string{"operator"}, "")
	token, csrf := authenticated(t, s, operator)
	m1 := resourceTestMerchant(t, srv, token, csrf, "Merchant one")
	m2 := resourceTestMerchant(t, srv, token, csrf, "Merchant two")
	binding := func(merchant string, in bindingInput, want int) string {
		t.Helper()
		status, out, _ := apiRequest(t, srv, "POST", "/api/v1/merchants/"+merchant+"/bindings", token, csrf, in)
		if status != want {
			t.Fatalf("binding %s status %d want %d (%v)", in.Kind, status, want, out["error"])
		}
		if status >= 400 {
			return ""
		}
		b := out["binding"].(map[string]any)
		if b["review_state"] != "pending_review" {
			t.Fatal("binding promoted to verified")
		}
		return b["id"].(string)
	}
	in := bindingInput{ProviderCode: "pyvio", InstanceCode: "sandbox-one", Kind: "enrollment", Label: "Unit", AccountScope: "contract-a", ExternalID: "private-unit-654321", EvidenceRef: "restricted/receipt/123", ProfileVersion: 1}
	parent := binding(m1, in, 201)
	if binding(m1, in, 200) != parent {
		t.Fatal("exact replay changed binding ID")
	}
	binding(m2, in, 409)
	changed := in
	changed.ProfileVersion = 2
	binding(m1, changed, 409)
	changed = in
	changed.ProviderCode = "other"
	binding(m1, changed, 409)
	changed = in
	changed.ExternalID = "private-unit-reference-m2"
	parent2 := binding(m2, changed, 201)
	va := bindingInput{ProviderCode: in.ProviderCode, InstanceCode: in.InstanceCode, Kind: "virtual_account", Label: "VA", ExternalID: "private-va-149201", EvidenceRef: "restricted/receipt/321", ProfileVersion: 1, Asset: "HKD", ParentID: parent}
	id := binding(m1, va, 201)
	upper := va
	upper.ParentID = strings.ToUpper(parent)
	if upper.ParentID != parent {
		binding(m1, upper, 400)
	}
	if binding(m1, va, 200) != id {
		t.Fatal("VA replay changed identity")
	}
	changed = va
	changed.AccountScope = "spoofed-scope"
	binding(m1, changed, 409)
	changed = va
	changed.ParentID = parent2
	binding(m1, changed, 400)
	changed = va
	changed.InstanceCode = "another-instance"
	binding(m1, changed, 400)
	// The same upstream identifier may legitimately recur under a different provider instance.
	changed = in
	changed.InstanceCode = "another-instance"
	other := binding(m2, changed, 201)
	changed = va
	changed.InstanceCode = "another-instance"
	changed.ParentID = other
	binding(m2, changed, 201)
	status, out, _ := apiRequest(t, srv, "GET", "/api/v1/merchants/"+m1+"/resources", token, csrf, nil)
	if status != 200 {
		t.Fatal(status)
	}
	raw, _ := json.Marshal(out)
	for _, sensitive := range []string{in.AccountScope, in.EvidenceRef, va.ExternalID, parent2, other} {
		if bytes.Contains(raw, []byte(sensitive)) {
			t.Fatal("resource list disclosed raw or foreign data")
		}
	}
	if len(out["bindings"].([]any)) != 2 || len(out["profiles"].([]any)) != 1 {
		t.Fatal("missing owned resources")
	}
	var encrypted []byte
	if err := s.Pool.QueryRow(context.Background(), `SELECT details_enc FROM provider_bindings WHERE id=$1`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(va.ExternalID)) {
		t.Fatal("external reference stored in plaintext")
	}
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/merchants/"+m1+"/resources", "", "", nil)
	if status != 401 {
		t.Fatal("anonymous access", status)
	}
	maintenance, maintenanceCSRF := authenticated(t, s, seedUser(t, s, []string{"root"}, ""))
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/merchants/"+m1+"/bindings", maintenance, maintenanceCSRF, in)
	if status != 403 {
		t.Fatal("maintenance binding permission", status)
	}
}

func TestResourceTargetVersionsAreMerchantScoped(t *testing.T) {
	s, srv := testServer(t)
	operator := seedUser(t, s, []string{"operator"}, "")
	token, csrf := authenticated(t, s, operator)
	token2, csrf2 := authenticated(t, s, seedUser(t, s, []string{"operator"}, ""))
	m1 := resourceTestMerchant(t, srv, token, csrf, "Merchant one")
	m2 := resourceTestMerchant(t, srv, token, csrf, "Merchant two")
	path := "/api/v1/merchants/" + m1 + "/targets"
	in := targetInput{RequestID: newID(), Kind: "bank", Asset: s.intakeFiatAsset(), Label: "Bank beneficiary", Destination: "private-bank-1234567", EvidenceRef: "private/mail/T12"}
	status, out, _ := apiRequest(t, srv, "POST", path, token, csrf, in)
	if status != 201 {
		t.Fatal(status, out["error"])
	}
	first := out["target"].(map[string]any)
	id := first["target_id"].(string)
	firstVersion := first["id"].(string)
	if first["masked_destination"] != "••••4567" {
		t.Fatal("mask missing")
	}
	initial := in
	status, replay, _ := apiRequest(t, srv, "POST", path, token, csrf, in)
	if status != 200 || replay["target"].(map[string]any)["id"] != firstVersion {
		t.Fatal("retry did not return original target", status)
	}
	changed := in
	changed.Destination = "changed-destination"
	status, _, _ = apiRequest(t, srv, "POST", path, token, csrf, changed)
	if status != 409 {
		t.Fatal("changed payload replayed", status)
	}
	history := path + "/" + id + "/versions"
	in.ExpectedVersion = 1
	in.Destination = "private-bank-7654321"
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, auth := range [][2]string{{token, csrf}, {token2, csrf2}} {
		wg.Add(1)
		go func(a [2]string) {
			defer wg.Done()
			attempt := in
			attempt.RequestID = newID()
			status, _, _ := apiRequest(t, srv, "POST", history, a[0], a[1], attempt)
			statuses <- status
		}(auth)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for code := range statuses {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatal("concurrent revisions", counts)
	}
	status, out, _ = apiRequest(t, srv, "GET", history, token, csrf, nil)
	if status != 200 {
		t.Fatal(status)
	}
	versions := out["items"].([]any)
	if len(versions) != 2 {
		t.Fatal("history overwritten")
	}
	newest, oldest := versions[0].(map[string]any), versions[1].(map[string]any)
	if oldest["id"] != firstVersion || newest["version"] != float64(2) || newest["review_state"] != "pending_review" {
		t.Fatal("version snapshot inconsistent")
	}
	data, _ := json.Marshal(out)
	if bytes.Contains(data, []byte(in.Destination)) || bytes.Contains(data, []byte(in.EvidenceRef)) {
		t.Fatal("raw destination or evidence disclosed")
	}
	var enc []byte
	if err := s.Pool.QueryRow(context.Background(), `SELECT details_enc FROM payment_target_versions WHERE id=$1`, newest["id"]).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte(in.Destination)) {
		t.Fatal("plaintext destination")
	}
	foreign := "/api/v1/merchants/" + m2 + "/targets/" + id + "/versions"
	for _, method := range []string{"GET", "POST"} {
		status, _, _ = apiRequest(t, srv, method, foreign, token, csrf, in)
		if status != 404 {
			t.Fatalf("cross-merchant %s returned %d", method, status)
		}
	}
	status, replay, _ = apiRequest(t, srv, "POST", path, token, csrf, initial)
	if status != 200 || replay["target"].(map[string]any)["id"] != firstVersion {
		t.Fatal("replaying creation after revision must return v1", status)
	}
	in.RequestID = newID()
	in.ExpectedVersion = 0
	in.Asset = "USD"
	if in.Asset == s.intakeFiatAsset() {
		in.Asset = "HKD"
	}
	status, _, _ = apiRequest(t, srv, "POST", path, token, csrf, in)
	if status != 400 {
		t.Fatal("currency outside configured intake accepted", status)
	}
	in.Kind = "chain"
	in.Asset = "USDT"
	in.Network = ""
	in.ExpectedVersion = 0
	status, _, _ = apiRequest(t, srv, "POST", path, token, csrf, in)
	if status != 400 {
		t.Fatal("chain target with unknown network accepted", status)
	}
}
