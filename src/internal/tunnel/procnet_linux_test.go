package tunnel

import (
	"bufio"
	"strings"
	"testing"
)

const tcpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1389 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1389 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:B4DE 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 333 1 0000000000000000 100 0 0 10 0
`

func TestParseListeners(t *testing.T) {
	out := map[uint64]bool{}
	parseListeners(bufio.NewScanner(strings.NewReader(tcpTable)), 5001, out)
	if len(out) != 1 || !out[111] {
		t.Fatalf("ожидался только слушающий 111: %v", out)
	}
}
