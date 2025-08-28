# flagd REST API Documentation

This document describes all REST operations provided by the `flagd` system.

## OFREP (OpenFeature Remote Evaluation Protocol) - Port 8016

### Single Flag Evaluation
- **POST** `/ofrep/v1/evaluate/flags/{flagKey}`
  - Evaluates a single feature flag
  - Example: `curl -X POST 'http://localhost:8016/ofrep/v1/evaluate/flags/myBoolFlag' -d '{"context":{}}'`

### Bulk Flag Evaluation  
- **POST** `/ofrep/v1/evaluate/flags`
  - Evaluates all configured flags in bulk
  - Example: `curl -X POST 'http://localhost:8016/ofrep/v1/evaluate/flags' -d '{"context":{}}'`

## HTTP/JSON-RPC Evaluation Service - Port 8013

### Flag Resolution Endpoints
- **POST** `/flagd.evaluation.v1.Service/ResolveBoolean`
  - Resolves boolean flags
  - Example: `curl -X POST "http://localhost:8013/flagd.evaluation.v1.Service/ResolveBoolean" -d '{"flagKey":"myBoolFlag","context":{}}' -H "Content-Type: application/json"`

- **POST** `/flagd.evaluation.v1.Service/ResolveString`
  - Resolves string flags
  - Example: `curl -X POST "http://localhost:8013/flagd.evaluation.v1.Service/ResolveString" -d '{"flagKey":"myStringFlag","context":{}}' -H "Content-Type: application/json"`

- **POST** `/flagd.evaluation.v1.Service/ResolveInt`
  - Resolves integer flags

- **POST** `/flagd.evaluation.v1.Service/ResolveFloat`
  - Resolves float flags

- **POST** `/flagd.evaluation.v1.Service/ResolveObject`
  - Resolves object/JSON flags

- **POST** `/flagd.evaluation.v1.Service/ResolveAll`
  - Resolves all flags in bulk

## Management/Health Endpoints - Port 8014

### Health Checks
- **GET** `/healthz`
  - Liveness probe - returns HTTP 200 when flagd service is running

- **GET** `/readyz`
  - Readiness probe - returns HTTP 200 when all sync providers have completed at least one successful sync, HTTP 412 otherwise

### Monitoring
- **GET** `/metrics`
  - Prometheus metrics endpoint for telemetry data

## Port Configuration

- OFREP service: Port 8016 (configurable with `--ofrep-port` or `-r`)
- HTTP evaluation: Port 8013 (default)
- Management/health: Port 8014 (default)

## Notes

- All endpoints support standard HTTP status codes and JSON request/response formats
- OFREP endpoints follow the OpenFeature Remote Evaluation Protocol specification
- HTTP/JSON-RPC endpoints provide gRPC-over-HTTP functionality
- Health endpoints are designed for Kubernetes liveness and readiness probes
- Metrics endpoint provides Prometheus-compatible telemetry data