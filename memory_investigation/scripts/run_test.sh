#!/bin/bash

# Usage: run_test.sh <PROTOCOL> <CONFIG> <SCENARIO>
PROTOCOL=${1:-http}
CONFIG=${2:-baseline}
SCENARIO=${3:-High-Single}

CLUSTER_NAME="obi-memory-test"
NAMESPACE="default"

echo "=== Running Metric Test: Protocol=$PROTOCOL Config=$CONFIG Scenario=$SCENARIO ==="

if [ -z "$DOCKER_REGISTRY" ]; then
    echo "DOCKER_REGISTRY not set. Please set it to your artifact registry (e.g., us-central1-docker.pkg.dev/dashpole-dev/obi-benchmark)"
    exit 1
fi

if [ -z "$GCS_BUCKET" ]; then
    echo "GCS_BUCKET not set. Defaulting to gs://dashpole-dev-obi-artifacts"
    export GCS_BUCKET="gs://dashpole-dev-obi-artifacts"
fi

echo "Building and pushing custom workload image..."
pushd memory_investigation/workloads/generator > /dev/null
docker build -t ${DOCKER_REGISTRY}/memory-investigation-workload:latest .
docker push ${DOCKER_REGISTRY}/memory-investigation-workload:latest
popd > /dev/null

echo "Building and pushing OBI image..."
docker build -t ${DOCKER_REGISTRY}/ebpf-instrument:latest .
docker push ${DOCKER_REGISTRY}/ebpf-instrument:latest

echo "Deploying OTEL Collector..."
kubectl apply -f memory_investigation/manifests/otelcol.yaml
kubectl rollout status deployment/otelcol -n $NAMESPACE --timeout=120s || true

echo "Depoying Test Target Server for $PROTOCOL..."
if [ -f "memory_investigation/manifests/servers/${PROTOCOL}-server.yaml" ]; then
    kubectl apply -f memory_investigation/manifests/servers/${PROTOCOL}-server.yaml
    # We don't strictly wait here, the generator deployment will wait for it to be ready via ping loops
else
    echo "No specific server manifest found for $PROTOCOL, assuming it is handled via standard workload."
fi

echo "Deploying Workload Generator ($PROTOCOL)..."
export WORKLOAD_PROTOCOL=$PROTOCOL

case $SCENARIO in
  Idle)
    export WORKLOAD_CLIENT_REPLICAS=0
    export WORKLOAD_SERVER_REPLICAS=1
    export WORKLOAD_QPS=0
    ;;
  High-Single)
    export WORKLOAD_CLIENT_REPLICAS=1
    export WORKLOAD_SERVER_REPLICAS=1
    export WORKLOAD_QPS=1000
    ;;
  High-Multi)
    export WORKLOAD_CLIENT_REPLICAS=5
    export WORKLOAD_SERVER_REPLICAS=5
    export WORKLOAD_QPS=200
    ;;
  *)
    echo "Unknown SCENARIO: $SCENARIO. Valid options: Idle, High-Single, High-Multi."
    exit 1
    ;;
esac

case $PROTOCOL in
  http)  export WORKLOAD_TARGET_URL="http-server.default.svc.cluster.local:8080" ;;
  https) export WORKLOAD_TARGET_URL="https-server.default.svc.cluster.local:8443" ;;
  grpc)  export WORKLOAD_TARGET_URL="grpc-server.default.svc.cluster.local:9090" ;;
  redis) export WORKLOAD_TARGET_URL="redis-service.default.svc.cluster.local:6379" ;;
  mysql) export WORKLOAD_TARGET_URL="test:test@tcp(mysql-service.default.svc.cluster.local:3306)/testdb" ;;
  postgres) export WORKLOAD_TARGET_URL="postgres://test:test@postgres-service.default.svc.cluster.local:5432/testdb?sslmode=disable" ;;
  kafka) export WORKLOAD_TARGET_URL="kafka-service.default.svc.cluster.local:9092" ;;
esac

export DOCKER_REGISTRY WORKLOAD_CLIENT_REPLICAS WORKLOAD_SERVER_REPLICAS WORKLOAD_PROTOCOL WORKLOAD_TARGET_URL WORKLOAD_QPS
envsubst '${DOCKER_REGISTRY} ${WORKLOAD_CLIENT_REPLICAS} ${WORKLOAD_SERVER_REPLICAS} ${WORKLOAD_PROTOCOL} ${WORKLOAD_TARGET_URL} ${WORKLOAD_QPS}' < memory_investigation/manifests/workload-template.yaml | kubectl apply -f -
kubectl rollout status deployment/workload-client -n $NAMESPACE --timeout=120s || true

echo "Deploying OBI ($CONFIG)..."

export OBI_ENV_VARS=""
case $CONFIG in
  baseline)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "false"
EOF
)
    ;;
  baseline_reduced)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "false"
            - name: OTEL_EBPF_BPF_CAPACITY_FACTOR
              value: "10"
EOF
)
    ;;
  approach4)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "false"
            - name: OTEL_EBPF_BPF_RINGBUF_CAPACITY_FACTOR
              value: "10"
EOF
)
    ;;
  approach5)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "false"
            - name: OTEL_EBPF_BPF_ELASTIC_MAPS
              value: "true"
EOF
)
    ;;
  k8s_attributes)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "true"
EOF
)
    ;;
  go_tracers)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_TRACES_INSTRUMENTATIONS
              value: "go"
EOF
)
    ;;
  service_graph)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_METRICS_FEATURES
              value: "application,service_graph"
EOF
)
    ;;
  golden_signals)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
EOF
)
    ;;
  traces)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
EOF
)
    ;;
  context_disabled)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "disabled"
EOF
)
    ;;
  context_headers)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "headers"
EOF
)
    ;;
  context_tcp)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "tcp"
EOF
)
    ;;
  large_batching)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_BPF_WAKEUP_LEN
              value: "1000"
            - name: OTEL_EBPF_BPF_BATCH_LENGTH
              value: "10000"
EOF
)
    ;;
  large_caches)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_BPF_MYSQL_PREPARED_STATEMENTS_CACHE_SIZE
              value: "10000"
            - name: OTEL_KAFKA_TOPIC_UUID_CACHE_SIZE
              value: "10000"
EOF
)
    ;;
  payloads)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_HTTP_GRAPHQL_ENABLED
              value: "true"
EOF
)
    ;;
esac

export OBI_ENV_VARS
envsubst < memory_investigation/manifests/obi-template.yaml | kubectl apply -f -
kubectl rollout status daemonset/obi -n $NAMESPACE --timeout=120s || true

echo "All components deployed successfully. Launching in-cluster scraper job..."

export PROTOCOL CONFIG SCENARIO GCS_BUCKET
envsubst '${PROTOCOL} ${CONFIG} ${SCENARIO} ${GCS_BUCKET}' < memory_investigation/manifests/scraper-job-template.yaml | kubectl apply -f -

echo "Waiting for scraper job to collect metrics and upload to GCS..."
# Capture working set memory at halfway point
for i in {1..10}; do
  kubectl top pod -l app=obi -n $NAMESPACE --no-headers > /tmp/top_${PROTOCOL}_${CONFIG}_${SCENARIO}.txt 2>/dev/null && break
  sleep 10
done
gsutil cp /tmp/top_${PROTOCOL}_${CONFIG}_${SCENARIO}.txt ${GCS_BUCKET}/${PROTOCOL}/${CONFIG}/${SCENARIO}/top.txt || true

kubectl wait --for=condition=complete job/metric-scraper -n $NAMESPACE --timeout=300s || true

echo "Scraper job logs:"
kubectl logs job/metric-scraper -n $NAMESPACE

echo "Cleaning up scraper job..."
kubectl delete job metric-scraper -n $NAMESPACE --ignore-not-found=true


echo "Cleaning up current deployment to reset for next test run..."
kubectl delete daemonset/obi -n $NAMESPACE --ignore-not-found=true
kubectl delete deployment/workload-client deployment/workload-server -n $NAMESPACE --ignore-not-found=true
if [ -f "memory_investigation/manifests/servers/${PROTOCOL}-server.yaml" ]; then
    kubectl delete -f memory_investigation/manifests/servers/${PROTOCOL}-server.yaml --ignore-not-found=true
fi
sleep 5

echo "Test complete! Artifacts saved to $PROFILE_DIR"
