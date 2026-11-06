package pitstop

import (
	"slices"
	"testing"
)

func TestPreferHost(t *testing.T) {
	urls := []string{
		"https://edgedl.me.gvt1.com/a/1",
		"https://dl.google.com/a/1",
		"https://edgedl.me.gvt1.com/a/2",
		"https://dl.google.com/a/2",
	}
	for _, tc := range []struct {
		host string
		want []string
	}{
		{"dl.google.com", []string{urls[1], urls[3], urls[0], urls[2]}},
		{"edgedl.me.gvt1.com", []string{urls[0], urls[2], urls[1], urls[3]}},
		{"example.com", urls},
	} {
		if got := preferHost(urls, tc.host); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.host, got, tc.want)
		}
	}
	if urls[0] != "https://edgedl.me.gvt1.com/a/1" {
		t.Error("preferHost changed its argument")
	}
}
