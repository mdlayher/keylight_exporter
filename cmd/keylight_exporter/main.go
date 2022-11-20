// Command keylight_exporter implements a Prometheus exporter for Elgato Key
// Light devices.
package main

import (
	"flag"
	"log"
	"net/http"

	keylightexporter "github.com/mdlayher/keylight_exporter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	var (
		metricsAddr = flag.String("metrics.addr", ":9288", "address for Elgato Key Light exporter")
		metricsPath = flag.String("metrics.path", "/metrics", "URL path for surfacing collected metrics")
	)

	flag.Parse()

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	mux := http.NewServeMux()
	mux.Handle(*metricsPath, keylightexporter.NewHandler(reg, nil))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, *metricsPath, http.StatusMovedPermanently)
	})

	log.Printf("starting Elgato Key Light exporter on %q", *metricsAddr)

	if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
		log.Fatalf("cannot start Elgato Key Light exporter: %v", err)
	}
}
