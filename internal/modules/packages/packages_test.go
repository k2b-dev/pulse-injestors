package packages

import "testing"

func TestCountAptUpdates(t *testing.T) {
	got := countAptUpdates([]byte(`Listing...
openssl/jammy-updates 3.0 amd64 [upgradable from: 2.9]
WARNING: apt does not have a stable CLI interface.
bash/jammy 5.1 amd64 [upgradable from: 5.0]
`))
	if got != 2 {
		t.Fatalf("got=%d", got)
	}
}

func TestCountLineUpdatesSkipsMetadata(t *testing.T) {
	got := countLineUpdates([]byte(`Last metadata expiration check: 0:01:00 ago
kernel.x86_64 1.2 repo
Obsoleting Packages
vim.x86_64 9.0 repo
`))
	if got != 2 {
		t.Fatalf("got=%d", got)
	}
}
