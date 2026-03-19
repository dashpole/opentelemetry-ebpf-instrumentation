# Random Test Verification Report

We randomly selected 20 test configurations to verify that they successfully instrument the workload and produce metrics.

## Test 1: Protocol `redis`, Config `baseline`, QPS `0`
**Status**: ✅ Success (Baseline)
Baseline is not expected to produce workload metrics or spans. It ran successfully.

## Test 2: Protocol `https`, Config `payloads`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 44 specific protocol metric data points (https).

**Evidence Sample:**
```text
http_server_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="/",instance="default.http-server-5dc6946756-7hz6z.server",job="defa
```

## Test 3: Protocol `kafka`, Config `go_tracers`, QPS `0`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `kafka` specific metric data points found. (Expected e.g. messaging_)

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `kafka` specific eBPF metrics.
```

## Test 4: Protocol `mysql`, Config `go_tracers`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 36 specific protocol metric data points (mysql).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="INSERT",db_system_name="mysql",error_type="",instance="default.mysql-server-69c67674f5-9knq9.mysql",job="default/mysq
```

## Test 5: Protocol `grpc`, Config `k8s_attributes`, QPS `100`
**Status**: ✅ Success
**Metrics Result**: Collected 18 specific protocol metric data points (grpc).

**Evidence Sample:**
```text
rpc_client_duration_seconds_bucket{instance="default.workload-client-5b9454849c-f4t7t.client",job="default/workload-client",k8s_cluster_name="",k8s_container_name="client",k8s_cron
```

## Test 6: Protocol `postgres`, Config `golden_signals`, QPS `10`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `postgres` specific metric data points found. (Expected e.g. db_system_name="postgresql")

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `postgres` specific eBPF metrics.
```

## Test 7: Protocol `grpc`, Config `large_batching`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 18 specific protocol metric data points (grpc).

**Evidence Sample:**
```text
rpc_client_duration_seconds_bucket{instance="default.workload-client-76c6fdd4bf-f66g9.client",job="default/workload-client",k8s_cluster_name="",k8s_container_name="client",k8s_cron
```

## Test 8: Protocol `redis`, Config `context_tcp`, QPS `0`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `redis` specific metric data points found. (Expected e.g. db_system_name="redis")

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `redis` specific eBPF metrics.
```

## Test 9: Protocol `grpc`, Config `context_headers`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 18 specific protocol metric data points (grpc).

**Evidence Sample:**
```text
rpc_client_duration_seconds_bucket{instance="default.workload-client-688c6845d5-vm4gb.client",job="default/workload-client",k8s_cluster_name="",k8s_container_name="client",k8s_cron
```

## Test 10: Protocol `kafka`, Config `large_batching`, QPS `10`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `kafka` specific metric data points found. (Expected e.g. messaging_)

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `kafka` specific eBPF metrics.
```

## Test 11: Protocol `redis`, Config `service_graph`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 36 specific protocol metric data points (redis).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="get",db_system_name="redis",error_type="",instance="default.redis-server-74c67c4bfc-sfdtm.redis",job="default/redis-s
```

## Test 12: Protocol `mysql`, Config `large_batching`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 18 specific protocol metric data points (mysql).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="SELECT",db_system_name="other_sql",error_type="",instance="default.workload-client-65b856696-xrclb.client",job="defau
```

## Test 13: Protocol `postgres`, Config `traces`, QPS `0`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `postgres` specific metric data points found. (Expected e.g. db_system_name="postgresql")

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `postgres` specific eBPF metrics.
```

## Test 14: Protocol `redis`, Config `context_headers`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 72 specific protocol metric data points (redis).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="get",db_system_name="redis",error_type="",instance="default.redis-server-74c67c4bfc-7g7zt.redis",job="default/redis-s
```

## Test 15: Protocol `kafka`, Config `context_disabled`, QPS `1000`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `kafka` specific metric data points found. (Expected e.g. messaging_)

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `kafka` specific eBPF metrics.
```

## Test 16: Protocol `grpc`, Config `service_graph`, QPS `1000`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `grpc` specific metric data points found. (Expected e.g. rpc_client_duration_seconds)

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `grpc` specific eBPF metrics.
```

## Test 17: Protocol `mysql`, Config `service_graph`, QPS `100`
**Status**: ❌ Missing Protocol Metrics
**Metrics Result**: No `mysql` specific metric data points found. (Expected e.g. db_system_name="mysql")

**Evidence Sample:**
```text
Result: The test only collected generic workload generator metrics (like http_client), but missing intended `mysql` specific eBPF metrics.
```

## Test 18: Protocol `redis`, Config `large_batching`, QPS `100`
**Status**: ✅ Success
**Metrics Result**: Collected 90 specific protocol metric data points (redis).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="",db_system_name="redis",error_type="error",instance="default.workload-client-6dd6845d84-qtb8q.client",job="default/w
```

## Test 19: Protocol `mysql`, Config `context_disabled`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 18 specific protocol metric data points (mysql).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="INSERT",db_system_name="mysql",error_type="",instance="default.mysql-server-69c67674f5-xf5zd.mysql",job="default/mysq
```

## Test 20: Protocol `redis`, Config `context_disabled`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 72 specific protocol metric data points (redis).

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="get",db_system_name="redis",error_type="",instance="default.redis-server-74c67c4bfc-9nkln.redis",job="default/redis-s
```

