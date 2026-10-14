package build

import (
	"strings"
	"testing"
)

func TestParseBasic(t *testing.T) {
	src := "# comment\nFROM alpine:3.18\n\nWORKDIR /app\nENV A=b\nCOPY . /app\nCMD [\"x\", \"y\"]\n"
	ins, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 5 || ins[0].Line != 2 || ins[4].Line != 7 || ins[3].Raw != "COPY . /app" {
		t.Fatalf("bad parse: %+v", ins)
	}
	c, _ := ParseCmd(ins[4].Args)
	if len(c) != 2 || c[1] != "y" {
		t.Fatalf("cmd %v", c)
	}
	if ParseEnv(`K="a b"`) != "K=a b" {
		t.Fatal("env quote strip")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"FROM a\n\n# c\nEXPOSE 80\n", "line 4: unknown instruction"},
		{"FROM a\nCMD python main.py\n", "line 2: CMD requires JSON array"},
		{"RUN echo hi\n", "first instruction must be FROM"},
		{"FROM a\nFROM b\n", "line 2: multiple FROM"},
		{"FROM a\nENV nokv\n", "line 2: ENV requires KEY=VALUE"},
	}
	for _, c := range cases {
		_, err := Parse(strings.NewReader(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src %q: got %v, want %q", c.src, err, c.want)
		}
	}
}
