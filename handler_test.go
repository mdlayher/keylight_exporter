package keylightexporter_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/keylight"
	keylightexporter "github.com/mdlayher/keylight_exporter"
	"github.com/mdlayher/promtest"
	"github.com/prometheus/client_golang/prometheus"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name   string
		target string
		code   int
	}{
		{
			name: "no target",
			code: http.StatusBadRequest,
		},
		{
			name:   "bad scheme",
			target: "sftp://foo",
			code:   http.StatusBadRequest,
		},
		{
			name:   "bad host",
			target: "http://",
			code:   http.StatusBadRequest,
		},
		{
			name:   "bad port",
			target: "foo:bar",
			code:   http.StatusBadRequest,
		},
		{
			name:   "bad path",
			target: "http://foo/bar",
			code:   http.StatusBadRequest,
		},
		{
			name:   "OK host",
			target: "foo",
			code:   http.StatusOK,
		},
		{
			name:   "OK host:port",
			target: "foo:9123",
			code:   http.StatusOK,
		},
		{
			name:   "OK HTTP trailing slash",
			target: "http://foo:9123/",
			code:   http.StatusOK,
		},
		{
			name:   "OK HTTP",
			target: "http://foo:9123",
			code:   http.StatusOK,
		},
		{
			name:   "OK HTTPS",
			target: "https://foo:9123",
			code:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := testFetcher{
				fetch: func(_ context.Context, addr string) (*keylightexporter.Data, error) {
					// Assume all calls create a well-formed URL with scheme,
					// host, and port.
					u, err := url.Parse(addr)
					if err != nil {
						panicf("failed to parse URL: %v", err)
					}

					if u.Scheme != "http" && u.Scheme != "https" {
						panicf("bad URL scheme: %q", u.Scheme)
					}
					if diff := cmp.Diff("foo:9123", u.Host); diff != "" {
						panicf("unexpected URL host (-want +got):\n%s", diff)
					}
					if diff := cmp.Diff("", u.Path); diff != "" {
						t.Fatalf("unexpected URL path (-want +got):\n%s", diff)
					}

					return &keylightexporter.Data{
						Device: &keylight.Device{
							DisplayName:     "test",
							FirmwareVersion: "1.0.0",
							SerialNumber:    "1111",
						},
						Lights: []*keylight.Light{
							{
								On:          true,
								Brightness:  20,
								Temperature: 4200,
							},
							// A second light which is entirely off.
							{},
						},
					}, nil
				},
			}

			res := testHandler(t, fetcher, tt.target)
			defer res.Body.Close()

			if diff := cmp.Diff(tt.code, res.StatusCode); diff != "" {
				t.Fatalf("unexpected HTTP status code (-want +got):\n%s", diff)
			}

			if res.StatusCode != http.StatusOK {
				return
			}

			b, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("failed to read HTTP body: %v", err)
			}

			// TODO(mdlayher): re-enable when Kelvin is allowed as a metric unit.
			// https://github.com/prometheus/client_golang/pull/761
			/*
				if !promtest.Lint(t, b) {
					t.Fatal("failed to lint Prometheus metrics")
				}
			*/

			match := []string{
				`keylight_info{firmware="1.0.0",name="test",serial="1111"} 1`,
				`keylight_light_on{light="light0",serial="1111"} 1`,
				`keylight_light_brightness_percent{light="light0",serial="1111"} 20`,
				`keylight_light_color_temperature_kelvin{light="light0",serial="1111"} 4200`,
				`keylight_light_on{light="light1",serial="1111"} 0`,
				`keylight_light_brightness_percent{light="light1",serial="1111"} 0`,
				`keylight_light_color_temperature_kelvin{light="light1",serial="1111"} 0`,
			}

			if !promtest.Match(t, b, match) {
				t.Fatal("failed to match Prometheus metrics")
			}
		})
	}
}

type testFetcher struct {
	fetch func(ctx context.Context, addr string) (*keylightexporter.Data, error)
}

func (f testFetcher) Fetch(ctx context.Context, addr string) (*keylightexporter.Data, error) {
	return f.fetch(ctx, addr)
}

// testHandler performs a single HTTP request to a handler created using
// NewHandler, using the specified target.
func testHandler(t *testing.T, f keylightexporter.Fetcher, target string) *http.Response {
	t.Helper()

	srv := httptest.NewServer(keylightexporter.NewHandler(prometheus.NewPedanticRegistry(), f))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	q := u.Query()
	q.Set("target", target)
	u.RawQuery = q.Encode()

	c := &http.Client{Timeout: 1 * time.Second}
	res, err := c.Get(u.String())
	if err != nil {
		t.Fatalf("failed to perform HTTP request: %v", err)
	}

	return res
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
