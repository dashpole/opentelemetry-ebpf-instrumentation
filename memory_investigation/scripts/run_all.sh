#!/bin/bash

# Configuration Matrix driver
HTTP_CONFIGS="baseline k8s_attributes go_tracers service_graph golden_signals traces context_disabled context_headers context_tcp large_batching large_caches payloads"
OTHER_PROTOCOLS="https grpc mysql postgres redis kafka"
OTHER_CONFIGS="baseline golden_signals"
HTTP_SCENARIOS="Idle High-Single High-Multi"
OTHER_SCENARIOS="Idle High-Single"

echo "Starting Matrix driver..."

# 1. Exhaustive matrix for HTTP/1.1
for CONF in $HTTP_CONFIGS; do
  for SCENARIO in $HTTP_SCENARIOS; do
    ./memory_investigation/scripts/run_test.sh "http" "$CONF" "$SCENARIO"
  done
done

# 2. Baseline and Typical configuration for other protocols
for PROT in $OTHER_PROTOCOLS; do
  for CONF in $OTHER_CONFIGS; do
    for SCENARIO in $OTHER_SCENARIOS; do
      ./memory_investigation/scripts/run_test.sh "$PROT" "$CONF" "$SCENARIO"
    done
  done
done

echo "Matrix driver complete."
