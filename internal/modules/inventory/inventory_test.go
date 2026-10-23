package inventory

import (
	"context"
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryFixturesAndPackageFallback(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"etc/os-release": "NAME=\"Test OS\"\nVERSION_ID='1.2'\nPRETTY_NAME=\"Test OS 1.2\"\n", "sys/kernel/osrelease": "6.1\n", "cpuinfo": "processor : 0\nmodel name : Test CPU\n", "class/dmi/id/sys_vendor": "Vendor\n", "class/dmi/id/product_name": "Model\n"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := Collector{HostRoot: root, ProcRoot: root, SysRoot: root, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no timeout")
		}
		if name == "rpm" {
			return nil, exec.ErrNotFound
		}
		if name != "dpkg-query" {
			t.Fatal(name)
		}
		if strings.Join(args, "|") != "-W|-f|${db:Status-Abbrev}\t${Package}\t${Architecture}\t${Version}\n" {
			t.Fatalf("dpkg args: %q", args)
		}
		return []byte("ii \tbash\tamd64\t5.2\nhi \topenssl\tamd64\t3.0\nrc \tremoved\tamd64\t1.0\nun \tunknown\t\t\nmalformed\nii \tbroken\tamd64\t\n"), nil
	}}
	b, err := c.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "vm"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	for _, s := range b.States {
		values[s.Key] = s.Value
		if s.Key == "software.package.version" && (s.Dimensions["arch"] != "amd64" || s.Resource.Key() != "host:vm") {
			t.Fatal(s)
		}
	}
	if len(b.States) != 12 || values["software.packages.skipped"] != 2 || values["system.os.name"] != "Test OS" || values["system.hardware.cpu_model"] != "Test CPU" || b.Metrics[0].Value != 2 {
		t.Fatalf("batch=%+v", b)
	}
}
func TestPackageCapAndMalformedRows(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 5002; i++ {
		fmt.Fprintf(&body, "pkg%04d\tx86_64\t0:1-2\n", i)
	}
	b := monitoring.NewBuilder(monitoring.Scope{})
	c := Collector{Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(body.String()), nil }}
	if err := c.packages(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if len(b.Batch().States) != 5002 || b.Batch().Metrics[0].Value != 5002 || b.Batch().States[1].Value != true {
		t.Fatal("package cap/count wrong")
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("bad"), nil }
	b = monitoring.NewBuilder(monitoring.Scope{})
	if err := c.packages(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if b.Batch().States[1].Key != "software.packages.skipped" || b.Batch().States[1].Value != 1 || b.Batch().Metrics[0].Value != 0 {
		t.Fatalf("malformed row was not counted: %+v", b.Batch())
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, exec.ErrNotFound }
	if err := c.packages(context.Background(), monitoring.NewBuilder(monitoring.Scope{})); err != nil {
		t.Fatal(err)
	}
}

func TestParallelPackageVersionsAreStable(t *testing.T) {
	c := Collector{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("kernel\tx86_64\t0:6.9-1\nkernel\tx86_64\t0:6.10-1\nkernel\tx86_64\t0:6.9-1\n"), nil
	}}
	b := monitoring.NewBuilder(monitoring.Scope{})
	if err := c.packages(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if b.Batch().Metrics[0].Value != 1 || b.Batch().States[2].Value != "0:6.10-1, 0:6.9-1" {
		t.Fatalf("versions lost or unstable: %+v", b.Batch())
	}
}
