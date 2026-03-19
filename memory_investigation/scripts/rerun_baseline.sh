#!/bin/bash
PROTOCOLS="http https grpc mysql postgres redis kafka"
QPS_LEVELS="0 10 100 1000"

echo "Rerunning baseline tests..."

for PROT in $PROTOCOLS; do
  for QPS in $QPS_LEVELS; do
    ./memory_investigation/scripts/run_test.sh "$PROT" "baseline" "$QPS"
  done
done

echo "Baseline reruns complete."
