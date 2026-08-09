package outdiff

import (
	"reflect"
	"testing"
)

func TestNormalizeTrimsAndDropsTrailingBlanks(t *testing.T) {
	got := Normalize("web-01", "load: 0.1   \r\n\n  \n")
	want := []string{"load: 0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}

func TestNormalizeReplacesOwnNames(t *testing.T) {
	cases := []struct {
		name string
		host string
		text string
		want []string
	}{
		{"bare hostname", "web-01", "web-01 ready", []string{Placeholder + " ready"}},
		{"prompt", "web-01", "root@web-01:~$ uptime", []string{"root@" + Placeholder + ":~$ uptime"}},
		{"user and port stripped", "deploy@web-01:2222", "web-01 ready", []string{Placeholder + " ready"}},
		{"short label of fqdn", "web-01.example.com", "hostname is web-01", []string{"hostname is " + Placeholder}},
		{"adjacent occurrences", "web-01", "web-01 web-01", []string{Placeholder + " " + Placeholder}},
		{"longer name untouched", "web-01", "web-011 is another machine", []string{"web-011 is another machine"}},
		{"inside a word untouched", "db", "rdbms running", []string{"rdbms running"}},
		{"single letter host skipped", "a", "a cat sat", []string{"a cat sat"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.host, tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Normalize(%q, %q) = %q, want %q", tc.host, tc.text, got, tc.want)
			}
		})
	}
}

func TestNormalizeEmptyIsNil(t *testing.T) {
	if got := Normalize("web-01", "  \n \n"); got != nil {
		t.Fatalf("Normalize of blank text = %q, want nil", got)
	}
}

func TestGroupBucketsByNormalizedOutput(t *testing.T) {
	variants := Group([]Sample{
		{Host: "web-01", Text: "root@web-01:~$ ok"},
		{Host: "web-02", Text: "root@web-02:~$ ok"},
		{Host: "db-01", Text: "root@db-01:~$ FAIL"},
		{Host: "web-03", Text: "root@web-03:~$ ok"},
	})
	if len(variants) != 2 {
		t.Fatalf("got %d variants, want 2", len(variants))
	}
	if want := []string{"web-01", "web-02", "web-03"}; !reflect.DeepEqual(variants[0].Hosts, want) {
		t.Fatalf("majority hosts = %v, want %v", variants[0].Hosts, want)
	}
	if want := []string{"db-01"}; !reflect.DeepEqual(variants[1].Hosts, want) {
		t.Fatalf("outlier hosts = %v, want %v", variants[1].Hosts, want)
	}
	if want := []string{"root@" + Placeholder + ":~$ ok"}; !reflect.DeepEqual(variants[0].Lines, want) {
		t.Fatalf("normalized lines = %q, want %q", variants[0].Lines, want)
	}
	// Raw keeps the first host's own text, hostname and all.
	if want := []string{"root@web-01:~$ ok"}; !reflect.DeepEqual(variants[0].Raw, want) {
		t.Fatalf("raw lines = %q, want %q", variants[0].Raw, want)
	}
}

func TestGroupLargestFirstStableTies(t *testing.T) {
	variants := Group([]Sample{
		{Host: "a1", Text: "alpha"},
		{Host: "b1", Text: "beta"},
		{Host: "b2", Text: "beta"},
		{Host: "c1", Text: "gamma"},
	})
	if len(variants) != 3 {
		t.Fatalf("got %d variants, want 3", len(variants))
	}
	if variants[0].Hosts[0] != "b1" {
		t.Fatalf("largest group first: got %v", variants[0].Hosts)
	}
	// alpha came before gamma; the tie keeps that order.
	if variants[1].Hosts[0] != "a1" || variants[2].Hosts[0] != "c1" {
		t.Fatalf("tie order not stable: %v then %v", variants[1].Hosts, variants[2].Hosts)
	}
}

func TestGroupNoOutputIsOneVariant(t *testing.T) {
	variants := Group([]Sample{
		{Host: "a1", Text: ""},
		{Host: "b1", Text: "   \n"},
	})
	if len(variants) != 1 {
		t.Fatalf("got %d variants, want 1", len(variants))
	}
	if len(variants[0].Lines) != 0 {
		t.Fatalf("no-output lines = %q, want none", variants[0].Lines)
	}
}

func TestGroupEmpty(t *testing.T) {
	if got := Group(nil); len(got) != 0 {
		t.Fatalf("Group(nil) = %v, want none", got)
	}
}
