package geoblock_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	geoblock "github.com/PascalMinder/geoblock"
)

const (
	asnExampleIP     = "192.0.2.10"
	otherASNIP       = "198.51.100.20"
	unknownASNIP     = "203.0.113.30"
	invalidASNIP     = "203.0.113.40"
	blockedCountryIP = "203.0.113.50"
	exampleASN       = "AS64500"
	otherASN         = "AS64501"
)

// createMockGeoIPServer serves /country/{ip} and /asn/{ip} like the geoip service.
func createMockGeoIPServer(t *testing.T) *httptest.Server {
	t.Helper()

	countries := map[string]string{
		asnExampleIP:     "CH",
		otherASNIP:       "CH",
		unknownASNIP:     "CH",
		invalidASNIP:     "CH",
		blockedCountryIP: "CA",
	}
	asns := map[string]string{
		asnExampleIP:     exampleASN,
		otherASNIP:       otherASN,
		unknownASNIP:     "AS0",
		invalidASNIP:     "not-an-asn",
		blockedCountryIP: exampleASN,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		table := countries
		if strings.HasPrefix(r.URL.Path, "/asn/") {
			table = asns
		}

		value, ok := table[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(value + "\n"))
	}))
	t.Cleanup(server.Close)

	return server
}

func createASNTesterConfig(server *httptest.Server, blackListMode bool, asns ...string) *geoblock.Config {
	cfg := createTesterConfig()
	cfg.API = server.URL + "/country/{ip}"
	cfg.ASNAPI = server.URL + "/asn/{ip}"
	cfg.ASNBlackListMode = blackListMode
	cfg.Countries = []string{"CH"}
	cfg.ASN = asns

	return cfg
}

func serveIP(t *testing.T, handler http.Handler, ip string) int {
	t.Helper()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	req.Header.Add(xForwardedFor, ip)
	handler.ServeHTTP(recorder, req)

	return recorder.Result().StatusCode
}

func newHandler(t *testing.T, cfg *geoblock.Config) http.Handler {
	t.Helper()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})
	handler, err := geoblock.New(context.Background(), next, cfg, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func TestASNBlacklistMode(t *testing.T) {
	server := createMockGeoIPServer(t)
	handler := newHandler(t, createASNTesterConfig(server, true, exampleASN))

	if code := serveIP(t, handler, asnExampleIP); code != http.StatusForbidden {
		t.Errorf("blacklisted ASN: expected %d, got %d", http.StatusForbidden, code)
	}
	if code := serveIP(t, handler, otherASNIP); code != http.StatusOK {
		t.Errorf("other ASN: expected %d, got %d", http.StatusOK, code)
	}
	if code := serveIP(t, handler, unknownASNIP); code != http.StatusOK {
		t.Errorf("unknown ASN: expected %d, got %d", http.StatusOK, code)
	}
}

func TestASNWhitelistMode(t *testing.T) {
	server := createMockGeoIPServer(t)
	handler := newHandler(t, createASNTesterConfig(server, false, exampleASN))

	if code := serveIP(t, handler, asnExampleIP); code != http.StatusOK {
		t.Errorf("whitelisted ASN: expected %d, got %d", http.StatusOK, code)
	}
	if code := serveIP(t, handler, otherASNIP); code != http.StatusForbidden {
		t.Errorf("other ASN: expected %d, got %d", http.StatusForbidden, code)
	}
	if code := serveIP(t, handler, unknownASNIP); code != http.StatusForbidden {
		t.Errorf("unknown ASN: expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestASNCachedLookUp(t *testing.T) {
	server := createMockGeoIPServer(t)
	handler := newHandler(t, createASNTesterConfig(server, true, exampleASN))

	for i := 0; i < 2; i++ {
		if code := serveIP(t, handler, asnExampleIP); code != http.StatusForbidden {
			t.Errorf("request %d: expected %d, got %d", i, http.StatusForbidden, code)
		}
	}
}

func TestASNConfigNormalized(t *testing.T) {
	server := createMockGeoIPServer(t)
	handler := newHandler(t, createASNTesterConfig(server, true, " as64500 ", "64501"))

	if code := serveIP(t, handler, asnExampleIP); code != http.StatusForbidden {
		t.Errorf("lowercase ASN: expected %d, got %d", http.StatusForbidden, code)
	}
	if code := serveIP(t, handler, otherASNIP); code != http.StatusForbidden {
		t.Errorf("ASN without prefix: expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestASNCountryDeniedFirst(t *testing.T) {
	server := createMockGeoIPServer(t)
	handler := newHandler(t, createASNTesterConfig(server, false, exampleASN))

	if code := serveIP(t, handler, blockedCountryIP); code != http.StatusForbidden {
		t.Errorf("denied country with allowed ASN: expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestASNInvalidAPIResponse(t *testing.T) {
	server := createMockGeoIPServer(t)

	cfg := createASNTesterConfig(server, true, exampleASN)
	if code := serveIP(t, newHandler(t, cfg), invalidASNIP); code != http.StatusForbidden {
		t.Errorf("invalid ASN response: expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestASNInvalidAPIResponseIgnored(t *testing.T) {
	server := createMockGeoIPServer(t)

	cfg := createASNTesterConfig(server, true, exampleASN)
	cfg.IgnoreAPIFailures = true
	if code := serveIP(t, newHandler(t, cfg), invalidASNIP); code != http.StatusOK {
		t.Errorf("invalid ASN response with ignoreApiFailures: expected %d, got %d", http.StatusOK, code)
	}
}

func TestASNBackfillsCachedEntry(t *testing.T) {
	server := createMockGeoIPServer(t)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})

	// first instance caches the entry without ASN
	cfg := createASNTesterConfig(server, true)
	cfg.ASNAPI = ""
	withoutASN, err := geoblock.New(context.Background(), next, cfg, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if code := serveIP(t, withoutASN, asnExampleIP); code != http.StatusOK {
		t.Fatalf("without ASN filter: expected %d, got %d", http.StatusOK, code)
	}

	// second instance shares the cache (same name) and must fetch the missing ASN
	withASN, err := geoblock.New(context.Background(), next, createASNTesterConfig(server, true, exampleASN), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if code := serveIP(t, withASN, asnExampleIP); code != http.StatusForbidden {
		t.Errorf("with ASN filter: expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestASNInvalidConfig(t *testing.T) {
	server := createMockGeoIPServer(t)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})

	tests := map[string]func(cfg *geoblock.Config){
		"asn api without asn list": func(cfg *geoblock.Config) { cfg.ASN = nil },
		"asn list without asn api": func(cfg *geoblock.Config) { cfg.ASNAPI = "" },
		"asn api without ip":       func(cfg *geoblock.Config) { cfg.ASNAPI = server.URL + "/asn/" },
		"invalid asn entry":        func(cfg *geoblock.Config) { cfg.ASN = []string{"ASX"} },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := createASNTesterConfig(server, true, exampleASN)
			mutate(cfg)
			if _, err := geoblock.New(context.Background(), next, cfg, t.Name()); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
