package systemd

import "testing"

func TestCleanUnits(t *testing.T) {
	got := cleanUnits([]string{" ssh.service ", "", "docker.service", "ssh.service"})
	if len(got) != 2 || got[0] != "ssh.service" || got[1] != "docker.service" {
		t.Fatalf("got=%v", got)
	}
}

func TestKeyValues(t *testing.T) {
	got := keyValues([]byte("LoadState=loaded\nActiveState=active\nDescription=A=B\n"))
	if got["LoadState"] != "loaded" || got["Description"] != "A=B" {
		t.Fatalf("got=%v", got)
	}
}
