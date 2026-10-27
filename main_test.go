package main

import (
	"flag"
	"strings"
	"testing"
)

func TestFlagsFirst(t *testing.T) {
	cases := []struct {
		args     []string
		note     string
		asJSON   bool
		position string
	}{
		{[]string{"cnv_1", "--note", "decided"}, "decided", false, "cnv_1"},
		{[]string{"--note", "decided", "cnv_1"}, "decided", false, "cnv_1"},
		{[]string{"cnv_1", "--json", "--note=x y"}, "x y", true, "cnv_1"},
		{[]string{"cnv_1"}, "", false, "cnv_1"},
		{[]string{"--", "-cnv"}, "", false, "-cnv"},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("close", flag.ContinueOnError)
		note := fs.String("note", "", "")
		asJSON := fs.Bool("json", false, "")
		if err := fs.Parse(flagsFirst(fs, c.args)); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if *note != c.note || *asJSON != c.asJSON || fs.Arg(0) != c.position {
			t.Fatalf("%v: note=%q json=%v args=%v", c.args, *note, *asJSON, strings.Join(fs.Args(), ","))
		}
	}
}
