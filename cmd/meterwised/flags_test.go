package main

import (
	"bytes"
	"flag"
	"testing"
)

func TestParseFlagsOwnsJSONAndRegisteredFlags(t *testing.T) {
	var email string
	register := func(fs *flag.FlagSet) { fs.StringVar(&email, "email", "", "") }
	cases := []struct {
		name     string
		args     []string
		wantJSON bool
		wantRest []string
		wantCode int
		wantOut  string // substring of the JSON error on stdout when -json was asked
	}{
		{"json anywhere with positionals", []string{"-json", "check"}, true, []string{"check"}, exitOK, ""},
		{"positional then json", []string{"check", "-json"}, true, []string{"check"}, exitOK, ""},
		{"registered flag", []string{"--email", "a@b.test", "-json"}, true, nil, exitOK, ""},
		{"unknown flag is a usage error", []string{"-nope"}, false, nil, exitUsage, ""},
		{"unknown flag with -json prints one JSON error", []string{"-json", "-nope"}, false, nil, exitUsage, `"code":"invalid_request"`},
		{"a flag with no value is a usage error", []string{"--email"}, false, nil, exitUsage, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			e := &env{stdout: &out, stderr: &errb}
			email = ""
			asJSON, rest, code := parseFlags(e, "x", c.args, register)
			if code != c.wantCode || (code == exitOK && (asJSON != c.wantJSON || len(rest) != len(c.wantRest))) {
				t.Fatalf("asJSON=%v rest=%v code=%d out=%q err=%q", asJSON, rest, code, out.String(), errb.String())
			}
			if c.wantOut != "" && !bytes.Contains(out.Bytes(), []byte(c.wantOut)) {
				t.Fatalf("stdout %q lacks %q", out.String(), c.wantOut)
			}
			if c.wantCode == exitUsage && c.wantOut == "" && out.Len() != 0 {
				t.Fatalf("no -json asked, yet stdout is %q", out.String())
			}
		})
	}
	var out bytes.Buffer
	if _, _, code := parseFlags(&env{stdout: &out, stderr: &out}, "x", []string{"--email", "kate@corp.test"}, register); code != exitOK || email != "kate@corp.test" {
		t.Fatalf("registered flag not parsed: code=%d email=%q", code, email)
	}
}
