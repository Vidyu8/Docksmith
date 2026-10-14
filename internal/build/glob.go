package build

import "path"

// matchSegs matches slash-split pattern segments against slash-split path
// segments. "*" / "?" / "[..]" work within one segment (path.Match);
// a "**" segment matches zero or more whole segments.
func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], segs[0])
	return err == nil && ok && matchSegs(pat[1:], segs[1:])
}
