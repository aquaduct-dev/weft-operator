// hostsfile.go — pure edits to node files the agent shares with other tools
// (/etc/hosts and the distro CA bundle). The agent owns a single tagged
// block per reflector per file and never touches any other line. PEM
// parsers skip text outside BEGIN/END CERTIFICATE, so the "#" marker lines
// are harmless in a CA bundle.
package nodeagent

import (
	"fmt"
	"strings"
)

func blockBegin(reflector string) string {
	return fmt.Sprintf("# BEGIN weft-local-dns-reflector %s (managed by weft-operator; do not edit)", reflector)
}

func blockEnd(reflector string) string {
	return fmt.Sprintf("# END weft-local-dns-reflector %s", reflector)
}

func caBlockBegin(reflector string) string {
	return fmt.Sprintf("# BEGIN weft-local-dns-reflector %s CA (managed by weft-operator; do not edit)", reflector)
}

func caBlockEnd(reflector string) string {
	return fmt.Sprintf("# END weft-local-dns-reflector %s CA", reflector)
}

// withHostsBlock returns content with the reflector's block set to map
// hostnames to ip, or removed entirely when hostnames is empty.
func withHostsBlock(content, reflector, ip string, hostnames []string) string {
	var body []string
	if len(hostnames) > 0 {
		body = []string{ip + " " + strings.Join(hostnames, " ")}
	}
	return withManagedBlock(content, blockBegin(reflector), blockEnd(reflector), body)
}

// withCABlock returns a CA bundle with the reflector's CAs appended in a
// managed block, or the block removed when caPEM is empty.
func withCABlock(content, reflector string, caPEM []byte) string {
	var body []string
	if trimmed := strings.TrimSpace(string(caPEM)); trimmed != "" {
		body = strings.Split(trimmed, "\n")
	}
	return withManagedBlock(content, caBlockBegin(reflector), caBlockEnd(reflector), body)
}

// withManagedBlock sets the begin..end block to body (removing it when body
// is empty). An existing block is replaced in place; a new one is appended.
func withManagedBlock(content, begin, end string, body []string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}

	var block []string
	if len(body) > 0 {
		block = append(append([]string{begin}, body...), end)
	}

	// Drop every complete BEGIN..END block for this reflector, remembering
	// where the first one was so the new block goes back in the same place.
	// A BEGIN with no matching END loses only the marker line itself: the
	// lines after it belong to someone else.
	var out []string
	insertAt := -1
	for i := 0; i < len(lines); i++ {
		if lines[i] != begin {
			out = append(out, lines[i])
			continue
		}
		j := i + 1
		for j < len(lines) && lines[j] != end && lines[j] != begin {
			j++
		}
		if insertAt < 0 {
			insertAt = len(out)
		}
		if j < len(lines) && lines[j] == end {
			i = j
		}
	}
	if insertAt < 0 {
		insertAt = len(out)
	}
	out = append(out[:insertAt], append(block, out[insertAt:]...)...)
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}
