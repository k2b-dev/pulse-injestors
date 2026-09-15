package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestReadMounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mountinfo")
	body := "34 23 0:29 / / rw,relatime - ext4 /dev/sda1 rw\n35 34 0:30 / /with\\040space rw - btrfs /dev/sdb rw\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mounts, err := ReadMounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("len = %d", len(mounts))
	}
	if mounts[1].Point != "/with space" || mounts[1].FSType != "btrfs" {
		t.Fatalf("mount = %#v", mounts[1])
	}
}

func TestFilesystemScopeUsesResourceEntity(t *testing.T) {
	scope := filesystemScope(monitoring.Scope{
		Resource: pulse.ResourceRef{Type: "host", ID: "server-01"},
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}, Mount{Point: "/var/lib/docker"})

	if scope.Resource.Type != "filesystem" || scope.Resource.Key() != "filesystem:server-01:%2Fvar%2Flib%2Fdocker" {
		t.Fatalf("scope = %#v", scope)
	}
}
