# keylight_exporter [![Linux Test Status](https://github.com/mdlayher/keylight_exporter/workflows/Linux%20Test/badge.svg)](https://github.com/mdlayher/keylight_exporter/actions) [![Go Report Card](https://goreportcard.com/badge/github.com/mdlayher/keylight_exporter)](https://goreportcard.com/report/github.com/mdlayher/keylight_exporter)

Command `keylight_exporter` implements a Prometheus exporter for Elgato Key
Light devices. MIT Licensed.

## Configuration

The `keylight_exporter`'s Prometheus scrape configuration (`prometheus.yml`) is
configured in a similar way to the official Prometheus
[`blackbox_exporter`](https://github.com/prometheus/blackbox_exporter).

The `targets` list under `static_configs` should specify the addresses of any
keylight devices which should be monitored by the exporter.  The address of
the `keylight_exporter` itself must be specified in `relabel_configs` as well.

```yaml
scrape_configs:
  - job_name: 'keylight'
    static_configs:
      - targets:
        - '192.168.1.10' # keylight device.
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: '127.0.0.1:9288' # keylight_exporter.
```
