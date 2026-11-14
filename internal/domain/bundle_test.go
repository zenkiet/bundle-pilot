package domain

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshotServesOnlyTheDefaultsBase(t *testing.T) {
	inv := Inventory{
		Bundles: []*Bundle{{Version: "3.23.0", base: "/"}, {Version: "4.0.0", base: "/client/"}},
		Config:  Config{Default: "3.23.0", Rules: []Rule{{ID: "big-bang", Bundle: "4.0.0"}}},
	}
	s := NewSnapshot(inv, time.Now(), "")
	if s.BasePath != "/" || s.Bundle("3.23.0") == nil {
		t.Fatalf("base %q, 3.23.0 served %v", s.BasePath, s.Bundle("3.23.0") != nil)
	}
	if s.Bundle("4.0.0") != nil || s.Config.Rules[0].Active {
		t.Fatal("4.0.0, built for /client/, is served or a rule routes to it")
	}
	var texts []string
	for _, issue := range s.Issues {
		texts = append(texts, issue.Text)
	}
	issues := strings.Join(texts, "\n")
	for _, want := range []string{"bundle 4.0.0 has <base href> /client/, the gateway serves /", "rule big-bang: bundle 4.0.0 has another <base href>"} {
		if !strings.Contains(issues, want) {
			t.Errorf("issues lack %q:\n%s", want, issues)
		}
	}

	inv.Bundles[0].base = "/client/"
	if s := NewSnapshot(inv, time.Now(), ""); s.Bundle("4.0.0") == nil || !s.Config.Rules[0].Active {
		t.Fatal("bundles on one base: 4.0.0 is not served")
	}
}

func TestRefreshRenewsAnExpiredSessionFor30Days(t *testing.T) {
	a := Auth{Username: "admin", Hash: "h"}
	login := time.Now().Add(-29 * 24 * time.Hour)
	token, _ := a.Session(login)
	if _, ok := a.SessionExpiry(token); ok {
		t.Fatal("a 29-day-old token still authorizes calls")
	}
	fresh, exp, err := a.Refresh(token, time.Now())
	if err != nil {
		t.Fatalf("Refresh of an expired token inside 30 days: %v", err)
	}
	if _, ok := a.SessionExpiry(fresh); !ok || exp.After(login.Add(sessionMax)) {
		t.Errorf("fresh token expires %v (valid %v), want it valid and no later than 30 days after the login", exp, ok)
	}
	if _, _, err := a.Refresh(token, login.Add(sessionMax)); err == nil {
		t.Error("Refresh 30 days after the login succeeded")
	}
	if _, _, err := (Auth{Username: "admin", Hash: "changed"}).Refresh(token, time.Now()); err == nil {
		t.Error("Refresh after a password change succeeded")
	}
}
