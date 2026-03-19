# Random Test Verification Report

We randomly selected 20 test configurations to verify that they successfully instrument the workload and produce metrics.

## Test 1: Protocol `redis`, Config `baseline`, QPS `0`
**Status**: ✅ Success (Baseline)
Baseline is not expected to produce workload metrics or spans. It ran successfully.

## Test 2: Protocol `https`, Config `payloads`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 132 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 3: Protocol `kafka`, Config `go_tracers`, QPS `0`
**Status**: ✅ Success
**Metrics Result**: Collected 88 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 4: Protocol `mysql`, Config `go_tracers`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 142 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="INSERT",db_system_name="mysql",error_type="",instance="default.mysql-server-69c67674f5-9knq9.mysql",job="default/mysql-server",k8s_cluste
```

## Test 5: Protocol `grpc`, Config `k8s_attributes`, QPS `100`
**Status**: ✅ Success
**Metrics Result**: Collected 106 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 6: Protocol `postgres`, Config `golden_signals`, QPS `10`
**Status**: ❌ Warning
**Result**: No workload metric data points or spans were collected. The test might not have instrumented the workload correctly.

## Test 7: Protocol `grpc`, Config `large_batching`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 106 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 8: Protocol `redis`, Config `context_tcp`, QPS `0`
**Status**: ✅ Success
**Metrics Result**: Collected 88 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 9: Protocol `grpc`, Config `context_headers`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 106 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 10: Protocol `kafka`, Config `large_batching`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 88 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 11: Protocol `redis`, Config `service_graph`, QPS `1000`
**Status**: ❌ Warning
**Result**: No workload metric data points or spans were collected. The test might not have instrumented the workload correctly.

## Test 12: Protocol `mysql`, Config `large_batching`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 124 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="SELECT",db_system_name="other_sql",error_type="",instance="default.workload-client-65b856696-xrclb.client",job="default/workload-client",
```

## Test 13: Protocol `postgres`, Config `traces`, QPS `0`
**Status**: ✅ Success
**Metrics Result**: Collected 88 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 14: Protocol `redis`, Config `context_headers`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 160 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="get",db_system_name="redis",error_type="",instance="default.redis-server-74c67c4bfc-7g7zt.redis",job="default/redis-server",k8s_cluster_n
```

## Test 15: Protocol `kafka`, Config `context_disabled`, QPS `1000`
**Status**: ✅ Success
**Metrics Result**: Collected 88 workload metric data points.

**Evidence Sample:**
```text
http_client_request_body_size_bytes_bucket{http_request_method="GET",http_response_status_code="200",http_route="",instance="default.http-client-858c45464f-d9m49.client",job="default/http-client",k8s_
```

## Test 16: Protocol `grpc`, Config `service_graph`, QPS `1000`
**Status**: ❌ Warning
**Result**: No workload metric data points or spans were collected. The test might not have instrumented the workload correctly.

## Test 17: Protocol `mysql`, Config `service_graph`, QPS `100`
**Status**: ❌ Warning
**Result**: No workload metric data points or spans were collected. The test might not have instrumented the workload correctly.

## Test 18: Protocol `redis`, Config `large_batching`, QPS `100`
**Status**: ✅ Success
**Metrics Result**: Collected 178 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="",db_system_name="redis",error_type="error",instance="default.workload-client-6dd6845d84-qtb8q.client",job="default/workload-client",k8s_
```

## Test 19: Protocol `mysql`, Config `context_disabled`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 124 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="INSERT",db_system_name="mysql",error_type="",instance="default.mysql-server-69c67674f5-xf5zd.mysql",job="default/mysql-server",k8s_cluste
```

## Test 20: Protocol `redis`, Config `context_disabled`, QPS `10`
**Status**: ✅ Success
**Metrics Result**: Collected 160 workload metric data points.

**Evidence Sample:**
```text
db_client_operation_duration_seconds_bucket{db_operation_name="get",db_system_name="redis",error_type="",instance="default.redis-server-74c67c4bfc-9nkln.redis",job="default/redis-server",k8s_cluster_n
```
