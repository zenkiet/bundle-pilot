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
	issues := strings.Join(s.Issues, "\n")
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
