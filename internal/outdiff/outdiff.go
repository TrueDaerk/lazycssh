// Package outdiff groups hosts by the output they produced for the same
// command, so forty panes collapse into the handful of answers that actually
// differ. "Which machines disagree" is often the real reason for running a
// command everywhere; this package computes that answer (issue #46).
//
// Grouping compares *normalized* output: trailing whitespace is trimmed, and
// every occurrence of a host's own name is replaced by a placeholder, so a
// shell prompt or a `hostname`-bearing line does not put every host in its own
// group. Timestamps are deliberately not normalized in this iteration - there
// is no way to recognise them that does not also eat version numbers and IP
// addresses; a command whose output carries the clock will simply not
// converge, and the wiki says so.
package outdiff

import (
	"regexp"
	"strings"
)

// Placeholder is what a host's own name becomes in normalized output, so the
// same line said by different machines compares equal.
const Placeholder = "«host»"

// Sample is one host's raw output for the command under comparison.
type Sample struct {
	// Host is the session identifier, as the run names it - possibly
	// user@host:port.
	Host string
	// Text is the host's output, raw.
	Text string
}

// Variant is one distinct answer: the hosts that gave it, in the order their
// samples arrived, and the normalized lines they agreed on.
type Variant struct {
	// Hosts gave this output, in sample order.
	Hosts []string
	// Lines is the normalized output the hosts share.
	Lines []string
	// Raw is the first host's output before normalization, for display: the
	// user reads real hostnames, not placeholders.
	Raw []string
}

// Group buckets the samples by normalized output. Variants come back largest
// group first - the consensus leads, the outliers follow - with ties broken by
// which group's first sample came first, so the order is stable across
// recomputes.
func Group(samples []Sample) []Variant {
	index := make(map[string]int)
	var variants []Variant
	for _, s := range samples {
		lines := Normalize(s.Host, s.Text)
		key := strings.Join(lines, "\n")
		i, ok := index[key]
		if !ok {
			i = len(variants)
			index[key] = i
			variants = append(variants, Variant{Lines: lines, Raw: splitLines(s.Text)})
		}
		variants[i].Hosts = append(variants[i].Hosts, s.Host)
	}
	// Insertion sort by size, descending; equal sizes keep insertion order,
	// which is first-sample order. The variant count is small.
	for i := 1; i < len(variants); i++ {
		for j := i; j > 0 && len(variants[j].Hosts) > len(variants[j-1].Hosts); j-- {
			variants[j], variants[j-1] = variants[j-1], variants[j]
		}
	}
	return variants
}

// Normalize splits a host's output into comparison lines: trailing whitespace
// trimmed, trailing blank lines dropped, and the host's own names - the full
// identifier, the hostname inside it, and its first label - replaced by
// [Placeholder] so prompts and hostname echoes compare equal across machines.
func Normalize(host, text string) []string {
	lines := splitLines(text)
	re := selfPattern(host)
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		if re != nil {
			line = replaceSelf(line, re)
		}
		lines[i] = line
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// splitLines splits on newlines with carriage returns trimmed, and returns nil
// for empty text so "no output" is one shape, not two.
func splitLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}
	return lines
}

// selfPattern compiles the regexp matching a host's own names as whole words:
// the identifier as given, the hostname with any user@ and :port stripped, and
// that hostname's first DNS label. Names shorter than two characters are
// skipped - replacing every single letter would mangle the output worse than
// leaving a prompt unequal. Nil means the host contributes nothing to match.
func selfPattern(host string) *regexp.Regexp {
	seen := make(map[string]bool)
	var names []string
	add := func(n string) {
		if len(n) >= 2 && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	add(host)
	bare := host
	if i := strings.LastIndex(bare, "@"); i >= 0 {
		bare = bare[i+1:]
	}
	if i := strings.Index(bare, ":"); i >= 0 {
		bare = bare[:i]
	}
	add(bare)
	if i := strings.Index(bare, "."); i > 0 {
		add(bare[:i])
	}
	if len(names) == 0 {
		return nil
	}
	// Longest first, so web-01.example.com wins over web-01 where both match.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && len(names[j]) > len(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(strings.Join(quoted, "|"))
}

// replaceSelf substitutes the matches whose neighbours are not hostname
// characters. The boundary is checked by hand rather than in the pattern:
// regexp has no lookbehind, and a consumed boundary character would hide the
// next occurrence in "web-01 web-01".
func replaceSelf(line string, re *regexp.Regexp) string {
	matches := re.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		if !edge(line, m[0]-1) || !edge(line, m[1]) {
			continue
		}
		b.WriteString(line[last:m[0]])
		b.WriteString(Placeholder)
		last = m[1]
	}
	b.WriteString(line[last:])
	return b.String()
}

// edge reports whether the byte at i fails to extend a hostname: out of range,
// or not a word character, dot or dash - the characters hostnames are made of.
func edge(line string, i int) bool {
	if i < 0 || i >= len(line) {
		return true
	}
	c := line[i]
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '_' || c == '.' || c == '-':
		return false
	}
	return true
}
