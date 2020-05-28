package keylightexporter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mdlayher/keylight"
	"github.com/mdlayher/metricslite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// keylightPort is the default HTTP port used to communicate with Key Light
	// devices.
	keylightPort = "9123"

	// Prometheus metric names.
	klInfo                   = "keylight_info"
	klLightOn                = "keylight_light_on"
	klLightBrightnessPercent = "keylight_light_brightness_percent"
	klLightTemperatureKelvin = "keylight_light_temperature_kelvin"
)

var _ http.Handler = &handler{}

// A handler is an http.Handler that serves Prometheus metrics for Key Light
// devices.
type handler struct {
	f Fetcher

	mu      sync.Mutex
	mm      metricslite.Interface
	metrics http.Handler
}

// NewHandler returns an http.Handler that serves Prometheus metrics for Key
// Light devices. The Fetcher's Fetch method specifies how to connect to a
// device with the specified address on each HTTP request. If f is nil, a
// default HTTP fetcher will be used.
//
// Each HTTP request must contain a "target" query parameter which indicates the
// network address of the device which should be scraped for metrics. If no port
// is specified, the Key Light device default of 9123 will be used.
func NewHandler(reg *prometheus.Registry, f Fetcher) http.Handler {
	if f == nil {
		f = httpFetcher{}
	}

	mm := metricslite.NewPrometheus(reg)

	mm.ConstGauge(
		klInfo,
		"Metadata about an Elgato Key Light device.",
		"firmware", "name", "serial",
	)

	labels := []string{"light", "serial"}

	mm.ConstGauge(
		klLightOn,
		"Reports whether a given light on a device is turned on (0: off, 1: on).",
		labels...,
	)

	mm.ConstGauge(
		klLightBrightnessPercent,
		"The brightness percentage of a given light on a device.",
		labels...,
	)

	mm.ConstGauge(
		klLightTemperatureKelvin,
		"The color temperature in Kelvin of a given light on a device.",
		labels...,
	)

	return &handler{
		f:       f,
		mm:      mm,
		metrics: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	}
}

// A Fetcher can fetch Data about a Key Light device from addr.
type Fetcher interface {
	Fetch(ctx context.Context, addr string) (*Data, error)
}

// Data contains information which is used to export Prometheus metrics.
type Data struct {
	Device *keylight.Device
	Lights []*keylight.Light
}

// An httpFetcher uses a *keylight.Client to implement Fetcher.
type httpFetcher struct{}

// Fetch implements Fetcher.
func (httpFetcher) Fetch(ctx context.Context, addr string) (*Data, error) {
	c, err := keylight.NewClient(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %v", err)
	}

	d, err := c.AccessoryInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch device: %v", err)
	}

	ls, err := c.Lights(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lights: %v", err)
	}

	return &Data{
		Device: d,
		Lights: ls,
	}, nil
}

// ServeHTTP implements http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Prometheus is configured to send a target parameter with each scrape
	// request. This determines which device should be scraped for metrics.
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "missing target parameter", http.StatusBadRequest)
		return
	}

	addr, err := buildAddr(target)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("malformed target parameter: %v", err),
			http.StatusBadRequest,
		)
		return
	}

	d, err := h.f.Fetch(ctx, addr)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("failed to fetch Key Light data from %q: %v", addr, err),
			http.StatusInternalServerError,
		)
		return
	}

	// Ensure that concurrent requests for metrics for multiple devices are
	// serialized so the metrics do not get mismatched. This is necessary
	// because we are sharing the metrics handler for multiple requests rather
	// than creating a new one on each request.
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mm.OnConstScrape(scrapeDevice(d))
	h.metrics.ServeHTTP(w, r)
}

// buildAddr builds a well-formed HTTP endpoint address from s.
func buildAddr(s string) (string, error) {
	if !strings.Contains(s, "://") {
		// Assume that if no scheme is provided, this is host or host:port.
		return buildHostPort(s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}

	// Trim trailing slash for consistency.
	if u.Path == "/" {
		u.Path = ""
	}

	// Only allow HTTP(S) with non-empty host and empty path.
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return "", fmt.Errorf("invalid device URL: %q", u)
	}

	return u.String(), nil
}

// buildHostPort builds a well-formed HTTP endpoint from a string with no
// URL scheme.
func buildHostPort(s string) (string, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		// Assume no port was provided and use the default.
		host = s
		port = keylightPort
	}

	// Assume HTTP if no scheme provided and verify this URL is well formed
	// by verifying it again.
	s = (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
	}).String()

	return buildAddr(s)
}

// scrapeDevice gathers metrics for a single device's data.
func scrapeDevice(d *Data) metricslite.ScrapeFunc {
	serial := d.Device.SerialNumber

	return func(metrics map[string]func(value float64, labels ...string)) error {
		for name, c := range metrics {
			switch name {
			case klInfo:
				c(1.0, d.Device.FirmwareVersion, d.Device.DisplayName, serial)
			case klLightOn, klLightBrightnessPercent, klLightTemperatureKelvin:
				for i, l := range d.Lights {
					light := fmt.Sprintf("light%d", i)

					switch name {
					case klLightOn:
						c(boolFloat(l.On), light, serial)
					case klLightBrightnessPercent:
						c(float64(l.Brightness), light, serial)
					case klLightTemperatureKelvin:
						c(float64(l.Temperature), light, serial)
					default:
						panicf("keylight_exporter: unhandled light metric %q", name)
					}
				}
			default:
				panicf("keylight_exporter: unhandled metric %q", name)
			}
		}

		return nil
	}
}

// boolFloat converts b to a float64 0.0 or 1.0 value.
func boolFloat(b bool) float64 {
	if b {
		return 1.0
	}

	return 0.0
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
