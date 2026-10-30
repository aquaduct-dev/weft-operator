package weftserver

import "testing"

func TestProbedIP(t *testing.T) {
	for _, tc := range []struct{ name, logs, want string }{
		{
			// zerolog ConsoleWriter output of `weft probe`, colours included.
			name: "console log",
			logs: "\x1b[90m2026-10-30T02:00:00Z\x1b[0m \x1b[33mWRN\x1b[0m no candidate IPs found on eth1\n" +
				"\x1b[90m2026-10-30T02:00:00Z\x1b[0m \x1b[32mINF\x1b[0m \x1b[1mPerforming ACME probe\x1b[0m \x1b[36mbind_ip=\x1b[0m0.0.0.0 \x1b[36mhost=\x1b[0m198.51.100.10\n" +
				"\x1b[90m2026-10-30T02:00:01Z\x1b[0m \x1b[32mINF\x1b[0m \x1b[1mACME probe succeeded\x1b[0m \x1b[36mhost=\x1b[0m198.51.100.10\n",
			want: "198.51.100.10",
		},
		{name: "plain console log", logs: "2026-10-30T02:00:01Z INF ACME probe succeeded host=203.0.113.7\n", want: "203.0.113.7"},
		{name: "bare address", logs: "203.0.113.7\n", want: "203.0.113.7"},
		{name: "no success line", logs: "INF Performing ACME probe host=203.0.113.7\nFTL ACME probe failed host=203.0.113.7\n", want: ""},
		{name: "private address only", logs: "INF ACME probe succeeded host=10.0.0.1\n", want: ""},
		{name: "domain probe", logs: "INF ACME probe succeeded host=example.com\n", want: ""},
		{name: "empty", logs: "", want: ""},
	} {
		if got := probedIP(tc.logs); got != tc.want {
			t.Errorf("%s: probedIP = %q, want %q", tc.name, got, tc.want)
		}
	}
}
