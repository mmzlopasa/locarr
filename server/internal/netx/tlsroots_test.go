package netx

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestExtraRootsAreLoaded(t *testing.T) {
	entries, err := extraRoots.ReadDir("roots")
	if err != nil || len(entries) != 2 {
		t.Fatalf("roots: %v %d", err, len(entries))
	}
	pool := RootCAs()
	for _, e := range entries {
		b, _ := extraRoots.ReadFile("roots/" + e.Name())
		blk, _ := pem.Decode(b)
		if blk == nil {
			t.Fatalf("%s: no PEM", e.Name())
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if !c.IsCA || c.Subject.String() != c.Issuer.String() {
			t.Fatalf("%s is not a self-signed root: %s", e.Name(), c.Subject)
		}
		// A certificate signed by the root verifies against the pool.
		if _, err := c.Verify(x509.VerifyOptions{Roots: pool}); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
	}
	if Transport().(*lazyTransport).transport().TLSClientConfig.RootCAs != pool {
		t.Fatal("transport does not use the pool")
	}
}
