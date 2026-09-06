package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	info := func(version string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: version}, Settings: settings}, true
		}
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }
	cases := []struct {
		name, stamped string
		read          func() (*debug.BuildInfo, bool)
		want          string
	}{
		{"release stamp wins", "v1.2.3", info("v0.0.0-2026"), "v1.2.3"},
		{"go install module version", "", info("v0.3.0"), "v0.3.0"},
		{"checkout with commit", "", info("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"}), "dev (0123456)"},
		{"modified checkout", "", info("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"}, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "dev (0123456, modified)"},
		{"pseudo-version from an untagged checkout", "", info("v0.0.0-20260906171007-429639842187+dirty", debug.BuildSetting{Key: "vcs.revision", Value: "429639842187abcd"}, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "dev (4296398, modified)"},
		{"pseudo-version after a tag", "", info("v0.1.1-0.20260906172307-a9113694cf93", debug.BuildSetting{Key: "vcs.revision", Value: "a9113694cf93abcd"}), "dev (a911369)"},
		{"no build info", "", none, "dev"},
	}
	for _, tc := range cases {
		if got := versionString(tc.stamped, tc.read); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPickerShowsTheVersion(t *testing.T) {
	if !strings.Contains(plain(renderPicker(0, 0, 80, 24)), appVersion) {
		t.Fatalf("picker at 80 columns does not show version %q", appVersion)
	}
}
