#!/bin/bash

# Configuration Matrix driver
PROTOCOLS="http https grpc mysql postgres redis kafka"
CONFIGS="baseline k8s_attributes go_tracers service_graph golden_signals traces context_disabled context_headers context_tcp large_batching large_caches payloads"
QPS_LEVELS="0 10 100 1000"

# Note: The user requested NOT to run tests automatically yet, this script just drives the framework
# Uncomment the nested loop below to actually run the matrix.

echo "Starting Matrix driver..."

for PROT in $PROTOCOLS; do
  for CONF in $CONFIGS; do
    for QPS in $QPS_LEVELS; do
      ./memory_investigation/scripts/run_test.sh "$PROT" "$CONF" "$QPS"
    done
  done
done

echo "Matrix driver complete."
