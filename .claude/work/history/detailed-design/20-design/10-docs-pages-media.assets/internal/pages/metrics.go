package pages

import "go.opentelemetry.io/otel/attribute"

func attrResult(r string) attribute.KeyValue { return attribute.String("result", r) }
