package httpx

import metricdata "go.opentelemetry.io/otel/sdk/metric/metricdata"

type metricData struct{ v metricdata.ResourceMetrics }
