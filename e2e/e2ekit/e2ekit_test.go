package e2ekit

import (
	"strings"
	"testing"
)

const procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0
   1: DA002A0A:B4C2 FE1D4E05:2384 01 00000000:00000000 02:000AE3D2 00000000  1000        0 2 2 0000000000000000 20 4 30 10 -1
   2: DA002A0A:B4C4 FE1D4E05:2384 01 00000000:00000000 02:000AE3D2 00000000  1000        0 3 2 0000000000000000 20 4 30 10 -1
   3: DA002A0A:B4C6 FE1D4E05:2384 06 00000000:00000000 03:00000F37 00000000     0        0 0 3 0000000000000000
   4: DA002A0A:9A2E 0100002B:01BB 01 00000000:00000000 02:000AE3D2 00000000  1000        0 4 2 0000000000000000 20 4 30 10 -1
`

func TestCountEstablished(t *testing.T) {
	for port, want := range map[int]int{9092: 2, 443: 1, 0: 3, 80: 0} {
		got, err := countEstablished(strings.NewReader(procTCP), port)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("port %d: got %d, want %d", port, got, want)
		}
	}
}

func TestTail(t *testing.T) {
	if got := Tail("a\nb\nc\n", 2); got != "b\nc" {
		t.Errorf("Tail = %q", got)
	}
}
