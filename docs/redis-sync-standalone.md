# Standalone Redis Sync Service

The standalone Redis sync service allows you to run Redis synchronization as a separate process that exposes flags via gRPC. This enables multiple flagd instances to share a single Redis connection and provides better scalability and isolation.

## Overview

Instead of each flagd instance connecting directly to Redis, you can run a dedicated Redis sync service that:

1. Polls Redis for flag configuration changes
2. Exposes flags via gRPC sync protocol
3. Allows multiple flagd instances to connect as gRPC sync clients

## Benefits

### Scalability
- **Reduced Redis load**: One Redis connection serves multiple flagd instances
- **Horizontal scaling**: Add flagd instances without increasing Redis connections
- **Resource efficiency**: Centralized Redis polling reduces network traffic

### Reliability
- **Isolation**: Redis sync failures don't crash flag evaluation services
- **Independent scaling**: Scale Redis sync and flag evaluation independently
- **Graceful degradation**: Flagd continues serving cached flags if Redis sync is down

### Operational
- **Centralized monitoring**: Monitor Redis sync separately from flag evaluation
- **Independent updates**: Update Redis sync logic without restarting flagd
- **Network optimization**: Co-locate Redis sync service with Redis server

## Quick Start

### 1. Start Redis Sync Service

```bash
flagd redis-sync \
  --redis-uri="redis://localhost:6379/0?key=flags" \
  --redis-sync-port=8016 \
  --redis-interval=30
```

### 2. Start Flagd with gRPC Sync

```bash
flagd start \
  --sources='[{"uri":"localhost:8016","provider":"grpc"}]'
```

### 3. Store Flags in Redis

```bash
redis-cli set flags '{
  "flags": {
    "welcome-message": {
      "state": "ENABLED",
      "variants": {
        "on": "Hello World!",
        "off": "Goodbye World!"
      },
      "defaultVariant": "on"
    }
  }
}'
```

## Configuration

### Redis Sync Service Options

| Flag | Description | Default |
|------|-------------|---------|
| `--redis-uri` | Redis connection URI | Required |
| `--redis-interval` | Polling interval in seconds | 30 |
| `--redis-sync-port` | gRPC sync service port | 8016 |
| `--redis-sync-cert-path` | TLS certificate path | None |
| `--redis-sync-key-path` | TLS private key path | None |
| `--redis-sync-socket-path` | Unix socket path | None |
| `--redis-log-format` | Log format (console/json) | console |

### Redis URI Format

```
redis://[password@]host:port/database?key=flagkey
rediss://[password@]host:port/database?key=flagkey  # TLS enabled
```

### Flagd gRPC Sync Configuration

```bash
flagd start --sources='[{
  "uri": "redis-sync-service:8016",
  "provider": "grpc",
  "certPath": "/path/to/client.crt"  # Optional for TLS
}]'
```

## Deployment Examples

### Docker Compose

```yaml
version: '3.8'
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
  
  redis-sync:
    image: flagd:latest
    command: ["flagd", "redis-sync", 
              "--redis-uri=redis://redis:6379/0?key=flags",
              "--redis-sync-port=8016"]
    ports:
      - "8016:8016"
    depends_on:
      - redis
  
  flagd:
    image: flagd:latest
    command: ["flagd", "start",
              "--sources=[{\"uri\":\"redis-sync:8016\",\"provider\":\"grpc\"}]"]
    ports:
      - "8013:8013"  # gRPC evaluation
      - "8014:8014"  # HTTP evaluation
    depends_on:
      - redis-sync
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-sync-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis-sync-service
  template:
    metadata:
      labels:
        app: redis-sync-service
    spec:
      containers:
      - name: redis-sync
        image: flagd:latest
        command: ["flagd", "redis-sync"]
        args:
          - "--redis-uri=redis://redis-service:6379/0?key=flags"
          - "--redis-sync-port=8016"
        ports:
        - containerPort: 8016
          name: grpc-sync

---
apiVersion: v1
kind: Service
metadata:
  name: redis-sync-service
spec:
  selector:
    app: redis-sync-service
  ports:
  - port: 8016
    targetPort: 8016

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flagd
spec:
  replicas: 3  # Multiple instances sharing one Redis sync service
  selector:
    matchLabels:
      app: flagd
  template:
    metadata:
      labels:
        app: flagd
    spec:
      containers:
      - name: flagd
        image: flagd:latest
        command: ["flagd", "start"]
        args:
          - "--sources=[{\"uri\":\"redis-sync-service:8016\",\"provider\":\"grpc\"}]"
        ports:
        - containerPort: 8013
        - containerPort: 8014
```

## Monitoring
 docs/redis-sync-standalone.mdendpoints as flagd:

```bash
# Check if Redis sync service is ready
curl http://localhost:8014/readyz

# Check service metrics
curl http://localhost:8014/metrics
```

### Logging

Enable structured logging for better observability:

```bash
flagd redis-sync \
  --redis-uri="redis://localhost:6379/0?key=flags" \
  --redis-log-format=json
```

## Security

### TLS Configuration

Enable TLS for the gRPC sync service:

```bash
# Redis sync service with TLS
flagd redis-sync \
  --redis-uri="rediss://redis.example.com:6380/0?key=flags" \
  --redis-sync-cert-path=/etc/certs/server.crt \
  --redis-sync-key-path=/etc/certs/server.key

# Flagd client with TLS
flagd start \
  --sources='[{"uri":"redis-sync.example.com:8016","provider":"grpc","certPath":"/etc/certs/client.crt"}]'
```

## Troubleshooting

### Common Issues

1. **Connection refused**: Ensure Redis sync service is running and port is accessible
2. **Redis connection failed**: Check Redis URI and network connectivity
3. **TLS errors**: Verify certificate paths and permissions
4. **Sync delays**: Adjust `--redis-interval` for faster updates

### Debug Logging

Enable debug logging for troubleshooting:

```bash
flagd redis-sync \
  --redis-uri="redis://localhost:6379/0?key=flags" \
  --debug
```

## Migration from In-Process Redis Sync

To migrate from in-process Redis sync to standalone:

1. **Start Redis sync service** with same Redis URI
2. **Update flagd configuration** to use gRPC sync instead of Redis sync
3. **Test the setup** with a single flagd instance
4. **Scale horizontally** by adding more flagd instances

### Before (In-Process)
```bash
flagd start --sources='[{"uri":"redis://localhost:6379/0?key=flags","provider":"redis"}]'
```

### After (Standalone)
```bash
# Terminal 1: Redis sync service
flagd redis-sync --redis-uri="redis://localhost:6379/0?key=flags"

# Terminal 2: Flagd
flagd start --sources='[{"uri":"localhost:8016","provider":"grpc"}]'
```
