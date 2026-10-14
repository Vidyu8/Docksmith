package build

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Instruction is one parsed Docksmithfile line.
type Instruction struct {
	Op   string // upper-cased keyword
	Args string // text after the keyword
	Raw  string // full instruction text as written (trimmed); used in cache keys
	Line int
}

var known = map[string]bool{
	"FROM": true, "COPY": true, "RUN": true, "WORKDIR": true, "ENV": true, "CMD": true,
}

// Parse reads a Docksmithfile. Blank lines and #-comments are skipped but
// line numbers stay accurate. No line continuations (documented limitation).
func Parse(r io.Reader) ([]Instruction, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var out []Instruction
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		op, args := line, ""
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			op, args = line[:i], strings.TrimSpace(line[i:])
		}
		up := strings.ToUpper(op)
		if !known[up] {
			return nil, fmt.Errorf("line %d: unknown instruction %q", n, op)
		}
		if args == "" {
			return nil, fmt.Errorf("line %d: %s requires arguments", n, up)
		}
		ins := Instruction{Op: up, Args: args, Raw: line, Line: n}
		if err := validate(ins); err != nil {
			return nil, err
		}
		if up == "FROM" && len(out) > 0 {
			return nil, fmt.Errorf("line %d: multiple FROM (multi-stage builds) not supported", n)
		}
		out = append(out, ins)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Docksmithfile has no instructions")
	}
	if out[0].Op != "FROM" {
		return nil, fmt.Errorf("line %d: first instruction must be FROM", out[0].Line)
	}
	return out, nil
}

func validate(i Instruction) error {
	f := strings.Fields(i.Args)
	switch i.Op {
	case "FROM", "WORKDIR":
		if len(f) != 1 {
			return fmt.Errorf("line %d: %s takes exactly one argument", i.Line, i.Op)
		}
	case "COPY":
		if len(f) < 2 {
			return fmt.Errorf("line %d: COPY requires <src>... <dest>", i.Line)
		}
	case "ENV":
		k, _, ok := strings.Cut(i.Args, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return fmt.Errorf("line %d: ENV requires KEY=VALUE", i.Line)
		}
	case "CMD":
		if _, err := ParseCmd(i.Args); err != nil {
			return fmt.Errorf("line %d: %v", i.Line, err)
		}
	}
	return nil
}

// ParseCmd parses the JSON-array form: ["exe","arg"].
func ParseCmd(args string) ([]string, error) {
	var c []string
	if err := json.Unmarshal([]byte(args), &c); err != nil || len(c) == 0 {
		return nil, fmt.Errorf(`CMD requires JSON array form, e.g. CMD ["python", "main.py"]`)
	}
	return c, nil
}

// ParseEnv turns "KEY=VALUE" (optionally quoted value) into "KEY=VALUE".
func ParseEnv(args string) string {
	k, v, _ := strings.Cut(args, "=")
	k, v = strings.TrimSpace(k), strings.TrimSpace(v)
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	}
	return k + "=" + v
}
