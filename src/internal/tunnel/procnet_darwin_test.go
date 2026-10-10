package tunnel

import (
	"slices"
	"strings"
	"testing"
)

func TestParsePIDs(t *testing.T) {
	if got := parsePIDs("75770\n812\n\n"); !slices.Equal(got, []int{75770, 812}) {
		t.Fatalf("%v", got)
	}
	if got := parsePIDs(""); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestParseMarkerArgs(t *testing.T) {
	var h Holder
	parseMarkerArgs(strings.Fields("/usr/bin/ssh -o Tag=hotpipe:4242:t1 -N -T -- h"), &h)
	if h.Server != 4242 || h.Tunnel != "t1" {
		t.Fatalf("%+v", h)
	}
	// Тег без «-o» перед ним — не наш (например, имя хоста).
	var g Holder
	parseMarkerArgs(strings.Fields("ssh -- Tag=hotpipe:1:x"), &g)
	if g.Tunnel != "" {
		t.Fatalf("%+v", g)
	}
}
