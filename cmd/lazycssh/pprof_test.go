package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/program"
)

func TestStartPprofServesProfiles(t *testing.T) {
	addr, err := startPprof("127.0.0.1:0")
	if err != nil {
		t.Fatalf("startPprof: %v", err)
	}

	resp, err := http.Get("http://" + addr + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatalf("get goroutine profile: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("goroutine profile status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read profile body: %v", err)
	}
	if !strings.Contains(string(body), "goroutine profile:") {
		t.Fatalf("profile body does not look like a goroutine profile: %.80q", body)
	}
}

func TestStartPprofRefusesBadAddress(t *testing.T) {
	if _, err := startPprof("256.256.256.256:99999"); err == nil {
		t.Fatal("startPprof accepted an unusable address")
	}
}

func TestPprofFlagBadAddressFailsReadably(t *testing.T) {
	var stdout, stderr strings.Builder
	code := run([]string{"--pprof", "256.256.256.256:99999", "--sessions-dir", t.TempDir()},
		&stdout, &stderr, func(program.Config) error { return nil })
	if code != exitError {
		t.Fatalf("run = %d, want %d; stderr: %s", code, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "pprof") {
		t.Fatalf("stderr does not mention pprof: %s", stderr.String())
	}
}
