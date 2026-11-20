// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package obi // import "go.opentelemetry.io/obi/pkg/obi"

import (
	ebpfcommon "go.opentelemetry.io/obi/pkg/ebpf/common"
	"go.opentelemetry.io/obi/pkg/export/instrumentations"
)

// MapPruneOptions builds deterministic map-pruning options from the global
// OBI config so generictracer and gotracer apply identical rules to shared maps.
func (c *Config) MapPruneOptions() ebpfcommon.MapPruneOptions {
	if c == nil {
		return ebpfcommon.MapPruneOptions{
			ContextPropagationEnabled: true,
			HeaderPropagationEnabled:  true,
			TrackRequestHeaders:       true,
			PopulateTraceContext:      true,
			NodeJSEnabled:             true,
			NodeJSManualSpans:         true,
			JavaEnabled:               true,
			LargeBuffersEnabled:       true,
			AppRuntimeMetricsEnabled:  true,
			ExceptionsEnabled:         true,
			GoChannelLinksEnabled:     true,
			GoManualSDKEnabled:        true,
		}
	}

	buf := c.EBPF.BufferSizes
	largeBufs := buf.HTTP > 0 || buf.TCP > 0 || buf.MySQL > 0 || buf.Kafka > 0 ||
		buf.Postgres > 0 || buf.MSSQL > 0 || buf.Aerospike > 0

	tracesExported := c.Traces.Enabled() || c.TracePrinter.Enabled()
	spanMetrics := c.JoinMetricsConfig().Features.AnySpanMetrics() &&
		(c.OTELMetrics.EndpointEnabled() || c.Prometheus.EndpointEnabled())

	var protocols instrumentations.InstrumentationSelection
	if c.TracePrinter.Enabled() {
		protocols = instrumentations.NewInstrumentationSelection([]instrumentations.Instrumentation{
			instrumentations.InstrumentationALL,
		})
	} else {
		if c.Traces.Enabled() {
			protocols |= instrumentations.NewInstrumentationSelection(c.Traces.Instrumentations)
		}
		if c.OTELMetrics.EndpointEnabled() {
			protocols |= instrumentations.NewInstrumentationSelection(c.OTELMetrics.Instrumentations)
		}
		if c.Prometheus.EndpointEnabled() {
			protocols |= instrumentations.NewInstrumentationSelection(c.Prometheus.Instrumentations)
		}
	}

	return ebpfcommon.MapPruneOptions{
		ContextPropagationEnabled: c.EBPF.ContextPropagation.IsEnabled(),
		HeaderPropagationEnabled:  c.EBPF.ContextPropagation.HasHeaders(),
		TrackRequestHeaders:       c.EBPF.TrackRequestHeaders,
		PopulateTraceContext:      c.PopulateTraceContext(),
		NodeJSManualSpans:         c.NodeJS.ManualSpans,
		NodeJSEnabled:             c.NodeJS.Enabled,
		JavaEnabled:               c.Java.Enabled,
		LargeBuffersEnabled:       largeBufs,
		AppRuntimeMetricsEnabled:  c.AppRuntimeMetricsEnabled(),
		ExceptionsEnabled:         c.ExceptionsEnabled() && c.Logs.Enabled(),
		GoChannelLinksEnabled:     !c.EBPF.DisableGoChannelLinks && tracesExported,
		GoManualSDKEnabled:        tracesExported || spanMetrics,
		Protocols:                 protocols,
	}
}
