//go:build darwin

package peek

// The app in front on macOS: lsappinfo's front application — its bundle id and where it's
// installed. No permission needed (a window's title would need Screen Recording; Peek never asks).

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func NewSampler() (Sampler, string) { return darwinSampler{}, ModeFront }

type darwinSampler struct{}

var lsValue = regexp.MustCompile(`="([^"]*)"`)

func lsappinfo(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/lsappinfo", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (darwinSampler) Front() (Front, error) {
	asn := lsappinfo("front")
	if asn == "" || !strings.HasPrefix(asn, "ASN:") {
		return Front{}, nil
	}
	var f Front
	if m := lsValue.FindStringSubmatch(lsappinfo("info", "-only", "bundleid", asn)); m != nil {
		f.BundleID = m[1]
	}
	if m := lsValue.FindStringSubmatch(lsappinfo("info", "-only", "bundlepath", asn)); m != nil {
		f.Exe = m[1]
	}
	return f, nil
}

func (darwinSampler) Close() {}
