package main

import (
	"reflect"
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/config"
)

func TestLinuxSystemdUnitsMergesProfileAndConfiguredUnits(t *testing.T) {
	got := linuxSystemdUnits(config.Config{
		Linux: config.LinuxConfig{
			Profile:      "docker-host",
			SystemdUnits: []string{"docker.service", "custom.service"},
		},
	})
	want := []string{"docker.service", "containerd.service", "ssh.service", "sshd.service", "custom.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("units = %#v", got)
	}
}

func TestProfileSystemdUnitsUnknownProfileIsNoop(t *testing.T) {
	if got := profileSystemdUnits("unknown"); got != nil {
		t.Fatalf("units = %#v", got)
	}
}
