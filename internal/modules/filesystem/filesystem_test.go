package filesystem

import (
	"os"
	"path/filepath"
	"testing"
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
