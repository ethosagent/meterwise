package contract

import (
	"slices"
	"testing"
)

func TestTextHasherIsSplitInvariantAndTokenOnly(t *testing.T) {
	whole := NormalizedHash("Hello,  World! 42")
	var split TextHasher
	for _, p := range []string{"Hel", "lo,", "  Wor", "ld! ", "42"} {
		split.Write(p)
	}
	if whole == 0 || whole != split.Sum() {
		t.Fatalf("whole %d split %d", whole, split.Sum())
	}
	if NormalizedHash("hello world 42") != whole {
		t.Error("case and punctuation must not matter")
	}
	if NormalizedHash("hello world 43") == whole {
		t.Error("different tokens must differ")
	}
	if NormalizedHash(" .,! ") != 0 || NormalizedHash("") != 0 {
		t.Error("no token is hash 0")
	}
}

func TestShinglesAndTokens(t *testing.T) {
	toks := Tokens("One two THREE four five six seven eight nine ten", 0)
	if len(toks) != 10 || toks[2] != "three" {
		t.Fatalf("tokens = %v", toks)
	}
	if got := Tokens("a b c d", 2); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("limit: %v", got)
	}
	sh := ShinglesOf(toks, 100)
	if len(sh) != 3 {
		t.Fatalf("shingles = %d, want 3", len(sh))
	}
	if len(ShinglesOf(toks[:5], 100)) != 0 || len(ShinglesOf(toks, 2)) != 2 {
		t.Error("short input has none; max caps")
	}
	if !slices.Equal(TurnShingles("", "one two three four five six seven eight nine ten"), sh) {
		t.Error("TurnShingles over text only must equal the plain shingles")
	}
}

func TestClassifyTool(t *testing.T) {
	cases := map[string]ToolKind{
		"Read": ToolRead, "read_file": ToolRead, "readFile": ToolRead, "view": ToolRead, "get_file_contents": ToolRead,
		"Write": ToolWrite, "str_replace_editor": ToolWrite, "Bash": ToolWrite, "TodoWrite": ToolWrite, "read_and_write": ToolWrite,
		"grep": ToolOther, "get_weather": ToolOther, "thread_list": ToolOther, "locate": ToolOther,
	}
	for name, want := range cases {
		if got := ClassifyTool(name); got != want {
			t.Errorf("ClassifyTool(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestToolArgsHashIsCanonical(t *testing.T) {
	a := ToolArgsHash("read", []byte(`{"path":"/a","n":1}`))
	b := ToolArgsHash("read", []byte(` { "n" : 1, "path":"/a" } `))
	if a != b {
		t.Error("key order and whitespace must not matter")
	}
	if a == ToolArgsHash("read", []byte(`{"path":"/b","n":1}`)) || a == ToolArgsHash("cat", []byte(`{"path":"/a","n":1}`)) {
		t.Error("different args or name must differ")
	}
	if ToolArgsHash("x", []byte("not json")) == 0 {
		t.Error("invalid JSON still hashes")
	}
}

func TestToolPathAndHashID(t *testing.T) {
	if got := ToolPath([]byte(`{"offset":1,"file_path":"/x/y.go"}`)); got != "/x/y.go" {
		t.Errorf("path = %q", got)
	}
	if ToolPath([]byte(`{"q":"a"}`)) != "" || ToolPath([]byte(`[1]`)) != "" {
		t.Error("no path key is empty")
	}
	id := HashID("ws1", "v")
	if len(id) != 16 || id == HashID("ws2", "v") || id != HashID("ws1", "v") {
		t.Errorf("HashID %q must be 16 hex, per workspace, stable", id)
	}
	if HashValue(42) != "42" {
		t.Error("HashValue")
	}
}

func TestToolArgsHashShapes(t *testing.T) {
	nested := ToolArgsHash("t", []byte(`{"a":{"x":1,"y":[1, 2]},"b":"s p"}`))
	if nested != ToolArgsHash("t", []byte(`{ "b" : "s p", "a" : {"x":1,"y":[1,2]} }`)) {
		t.Error("top-level order and whitespace outside strings must not matter")
	}
	if nested == ToolArgsHash("t", []byte(`{"a":{"x":1,"y":[1,2]},"b":"sp"}`)) {
		t.Error("whitespace inside a string is data")
	}
	if ToolArgsHash("t", nil) != ToolArgsHash("t", []byte("  ")) || ToolArgsHash("t", nil) == ToolArgsHash("u", nil) {
		t.Error("empty args hash by name")
	}
	if ToolArgsHash("t", []byte(`[1, 2]`)) != ToolArgsHash("t", []byte(`[1,2]`)) || ToolArgsHash("t", []byte(`{"a":1`)) == 0 {
		t.Error("non-object and truncated arguments still hash")
	}
	if ToolArgsHash("t", []byte(`{"a":1}`)) == ToolArgsHash("t", []byte(`{"a":1,"b":null}`)) {
		t.Error("an extra member is a different call")
	}
	args := []byte(`{"path":"/a/b.go","offset":10,"opts":{"deep":[1,2,3]}}`)
	if n := testing.AllocsPerRun(100, func() { ToolArgsHash("read_file", args) }); n != 0 {
		t.Errorf("ToolArgsHash allocates %v times", n)
	}
}

func TestToolPathKeyPriorityAndShapes(t *testing.T) {
	cases := map[string]string{
		`{"file":"/f","path":"/p"}`:         "/p", // path outranks file whatever the order
		`{"path":"","file_path":"/fp"}`:     "/fp",
		`{"path":5,"file":"/f"}`:            "/f",
		`{"nested":{"path":"/no"}}`:         "",
		`{"path":"/with \"quote\" and \\"}`: `/with "quote" and \`,
		``:                                  "",
		`{"path":`:                          "",
	}
	for in, want := range cases {
		if got := ToolPath([]byte(in)); got != want {
			t.Errorf("ToolPath(%s) = %q, want %q", in, got, want)
		}
	}
}
