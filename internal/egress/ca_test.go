package egress

import (
	"crypto/x509"
	"testing"
)

func TestCAPersistsAndSignsLeaves(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca.PEM()) != string(again.PEM()) {
		t.Fatal("CA was regenerated instead of loaded")
	}
	for _, host := range []string{"api.example.com", "127.0.0.1"} {
		leaf, err := again.Leaf(host)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: ca.Pool(), DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
	}
	a, _ := ca.Leaf("cached.example")
	b, _ := ca.Leaf("cached.example")
	if a != b {
		t.Fatal("leaf not cached")
	}
}
