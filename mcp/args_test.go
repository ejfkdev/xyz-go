package mcp

import "testing"

func TestBareFlagPassthroughDefaults(t *testing.T) {
	_, opts, err := parseArgs([]string{"http", "--index", "./wiki", "--k=v"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Defaults["index"] != "./wiki" || opts.Defaults["k"] != "v" {
		t.Fatalf("passthrough defaults lost: %v", opts.Defaults)
	}
	// 缺值仍报错
	if _, _, err := parseArgs([]string{"http", "--dangling"}); err == nil {
		t.Fatal("dangling flag accepted")
	}
}
