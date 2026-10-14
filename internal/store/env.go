package store

import (
	"sort"
	"strings"
)

func envKey(kv string) string {
	if i := strings.Index(kv, "="); i >= 0 {
		return kv[:i]
	}
	return kv
}

// SetEnv sets kv ("K=V"), replacing an existing K in place, else appending.
func SetEnv(env []string, kv string) []string {
	k := envKey(kv)
	out := append([]string(nil), env...)
	for i, e := range out {
		if envKey(e) == k {
			out[i] = kv
			return out
		}
	}
	return append(out, kv)
}

// SortedEnv returns "K=V" lines sorted by key, newline-joined ("" if none).
// This is the exact ENV serialisation used in cache keys.
func SortedEnv(env []string) string {
	c := append([]string(nil), env...)
	sort.Slice(c, func(i, j int) bool { return envKey(c[i]) < envKey(c[j]) })
	return strings.Join(c, "\n")
}
