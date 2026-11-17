// Package duration parses and formats the CLI guide's duration grammar: one
// or more <n><unit> segments in d, h, m, s order, each unit at most once, or
// "0". time.ParseDuration accepts forms the grammar forbids, such as 1.5h,
// 300ms, and 30m1h, and rejects 7d.
package duration

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

type unit struct {
	suffix byte
	size   time.Duration
}

var units = []unit{
	{'d', 24 * time.Hour},
	{'h', time.Hour},
	{'m', time.Minute},
	{'s', time.Second},
}

// Parse returns the duration s denotes in the grammar.
func Parse(s string) (time.Duration, error) {
	if s == "0" {
		return 0, nil
	}
	bad := fmt.Errorf("invalid duration %q: use whole-number segments in d, h, m, s order, each once (30s, 1h30m, 7d) or 0", s)
	if s == "" {
		return 0, bad
	}
	var total time.Duration
	allowed := units
	for rest := s; rest != ""; {
		i := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
		if i <= 0 {
			return 0, bad
		}
		n, err := strconv.Atoi(rest[:i])
		if err != nil || n == 0 {
			return 0, bad
		}
		u := slices.IndexFunc(allowed, func(x unit) bool { return x.suffix == rest[i] })
		if u < 0 || time.Duration(n) > (math.MaxInt64-total)/allowed[u].size {
			return 0, bad
		}
		total += time.Duration(n) * allowed[u].size
		allowed = allowed[u+1:]
		rest = rest[i+1:]
	}
	return total, nil
}

// Format renders d in the grammar, rounding up to whole seconds so a short
// positive duration never renders as "0".
func Format(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	d = (d + time.Second - 1).Truncate(time.Second)
	var b strings.Builder
	for _, u := range units {
		if n := d / u.size; n > 0 {
			fmt.Fprintf(&b, "%d%c", n, u.suffix)
			d -= n * u.size
		}
	}
	return b.String()
}
