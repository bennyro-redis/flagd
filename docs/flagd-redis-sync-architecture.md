# Flagd Redis Sync Architecture

This document describes the processes and protocols for flag synchronization with Redis in the flagd system.

## Overview

Flagd supports Redis as a synchronization provider for feature flag configurations in two modes:

1. **In-Process Mode**: Redis sync provider runs embedded within flagd process
2. **Standalone Mode**: Redis sync runs as a separate gRPC service that flagd connects to

Both modes implement a polling-based approach with change detection to efficiently synchronize flag data from Redis.

## Architecture Diagrams

### In-Process Mode (Traditional)

```mermaid
graph TB
    %% External Systems
    Redis[(Redis Server)]
    Client[Flag Evaluation Client]
    InProcessClient[In-Process Provider]

    %% Flagd Process
    subgraph "Flagd Process"
        RedisSyncer[Redis Sync Provider<br/>polls every 30s]
        FlagStore[Flag Store<br/>In-Memory]
        GRPCServer[gRPC Server<br/>:8013 evaluation<br/>:8015 sync stream]
        HTTPServer[HTTP Server<br/>:8014 OFREP]
    end

    %% Data Flow
    Redis -->|JSON.GET/GET<br/>Redis Protocol| RedisSyncer
    RedisSyncer -->|Flag Updates<br/>In-Process| FlagStore

    %% Client Communication
    Client -->|gRPC Flag Evaluation<br/>:8013| GRPCServer
    Client -->|HTTP/REST Evaluation<br/>:8014| HTTPServer
    InProcessClient -->|gRPC Sync Stream<br/>:8015| GRPCServer

    %% Internal Evaluation
    GRPCServer -->|Query Flags| FlagStore
    HTTPServer -->|Query Flags| FlagStore
    FlagStore -->|Flag Data| GRPCServer
    FlagStore -->|Flag Data| HTTPServer

    %% Responses
    GRPCServer -->|Evaluation Result| Client
    HTTPServer -->|Evaluation Result| Client
    GRPCServer -->|Flag Stream| InProcessClient

    %% Styling
    classDef redisStyle fill:#dc382d,stroke:#333,stroke-width:2px,color:#fff
    classDef flagdStyle fill:#326ce5,stroke:#333,stroke-width:2px,color:#fff
    classDef clientStyle fill:#28a745,stroke:#333,stroke-width:2px,color:#fff

    class Redis redisStyle
    class RedisSyncer,FlagStore,GRPCServer,HTTPServer flagdStyle
    class Client,InProcessClient clientStyle
```

### Standalone Mode (New)

```mermaid
graph TB
    %% External Systems
    Redis[(Redis Server)]
    Client[Flag Evaluation Client]
    InProcessClient[In-Process Provider]

    %% Redis Sync Service Process
    subgraph "Redis Sync Service Process"
        RedisSyncer[Redis Sync Provider<br/>polls every 30s]
        SyncStore[Flag Store<br/>In-Memory]
        SyncGRPCServer[gRPC Sync Server<br/>:8016]
    end

    %% Flagd Process
    subgraph "Flagd Process"
        GRPCSyncer[gRPC Sync Provider<br/>connects to :8016]
        FlagStore[Flag Store<br/>In-Memory]
        GRPCServer[gRPC Server<br/>:8013 evaluation<br/>:8015 sync stream]
        HTTPServer[HTTP Server<br/>:8014 OFREP]
    end

    %% Data Flow
    Redis -->|JSON.GET/GET<br/>Redis Protocol| RedisSyncer
    RedisSyncer -->|Flag Updates<br/>In-Process| SyncStore
    SyncStore -->|Flag Data| SyncGRPCServer
    SyncGRPCServer -->|gRPC Sync Stream<br/>:8016| GRPCSyncer
    GRPCSyncer -->|Flag Updates<br/>In-Process| FlagStore

    %% Client Communication
    Client -->|gRPC Flag Evaluation<br/>:8013| GRPCServer
    Client -->|HTTP/REST Evaluation<br/>:8014| HTTPServer
    InProcessClient -->|gRPC Sync Stream<br/>:8015| GRPCServer

    %% Internal Evaluation
    GRPCServer -->|Query Flags| FlagStore
    HTTPServer -->|Query Flags| FlagStore
    FlagStore -->|Flag Data| GRPCServer
    FlagStore -->|Flag Data| HTTPServer

    %% Responses
    GRPCServer -->|Evaluation Result| Client
    HTTPServer -->|Evaluation Result| Client
    GRPCServer -->|Flag Stream| InProcessClient

    %% Styling
    classDef redisStyle fill:#dc382d,stroke:#333,stroke-width:2px,color:#fff
    classDef flagdStyle fill:#326ce5,stroke:#333,stroke-width:2px,color:#fff
    classDef syncStyle fill:#ff6b35,stroke:#333,stroke-width:2px,color:#fff
    classDef clientStyle fill:#28a745,stroke:#333,stroke-width:2px,color:#fff

    class Redis redisStyle
    class FlagStore,GRPCServer,HTTPServer,GRPCSyncer flagdStyle
    class RedisSyncer,SyncStore,SyncGRPCServer syncStyle
    class Client,InProcessClient clientStyle
```

## Key Components

### In-Process Mode
- **Redis Sync Provider**: Runs embedded within flagd process
- **Direct Integration**: Updates flagd's store directly
- **Simple Deployment**: Single process, easier to manage
- **Lower Latency**: No network hop between Redis sync and flag evaluation

### Standalone Mode
- **Redis Sync Service**: Separate process that polls Redis
- **gRPC Sync Server**: Exposes flags via gRPC sync protocol
- **Scalable**: Multiple flagd instances can connect to one Redis sync service
- **Isolation**: Redis sync failures don't affect flag evaluation

### Communication Protocols

#### Redis → Redis Sync Provider
- **Redis Protocol**: Standard Redis commands (`JSON.GET`, `GET`, `PING`)
- **Connection**: Direct TCP connection with optional TLS (`rediss://`)
- **Data Format**: JSON flag configuration stored in Redis key
- **Polling**: Configurable interval (default 30 seconds)
- **Change Detection**: SHA3-256 hashing to detect configuration changes

#### Redis Sync Service → Flagd (Standalone Mode Only)
- **gRPC Sync Protocol**: Real-time flag updates via streaming
- **Port**: Configurable (default 8016)
- **TLS Support**: Optional certificate-based encryption
- **Reconnection**: Automatic reconnection with backoff

#### Clients → Flagd
- **gRPC Evaluation** (port 8013): Flag evaluation requests
- **HTTP/OFREP** (port 8014): REST API for flag evaluation
- **gRPC Sync Stream** (port 8015): Real-time flag updates for in-process providers

## Usage Examples

### In-Process Mode (Traditional)
```bash
# Start flagd with Redis sync provider
flagd start --sources='[{"uri":"redis://localhost:6379/0?key=flags","provider":"redis","interval":30}]'
```

### Standalone Mode (New)
```bash
# 1. Start the standalone Redis sync service
flagd redis-sync --redis-uri="redis://localhost:6379/0?key=flags" --redis-sync-port=8016

# 2. Start flagd using the Redis sync service as a gRPC source
flagd start --sources='[{"uri":"localhost:8016","provider":"grpc"}]'
```

### Advanced Standalone Configuration
```bash
# Redis sync service with TLS and custom interval
flagd redis-sync \
  --redis-uri="rediss://user:pass@redis.example.com:6380/1?key=production-flags" \
  --redis-interval=15 \
  --redis-sync-port=8016 \
  --redis-sync-cert-path=/etc/certs/server.crt \
  --redis-sync-key-path=/etc/certs/server.key \
  --redis-log-format=json

# Multiple flagd instances connecting to the same Redis sync service
flagd start --sources='[{"uri":"redis-sync.example.com:8016","provider":"grpc","certPath":"/etc/certs/client.crt"}]'
```

## Configuration

### Redis URI Format
```
redis://[password@]host:port/database?key=flagkey
rediss://[password@]host:port/database?key=flagkey  # TLS enabled
```

### Key Parameters
- **Host/Port**: Redis server connection details
- **Database**: Redis database number (default: 0)
- **Key**: Redis key containing flag configuration
- **TLS**: Enabled via `rediss://` scheme
- **Authentication**: Password in URI userinfo section

### Standalone Mode Parameters
- **--redis-sync-port**: gRPC sync service port (default: 8016)
- **--redis-interval**: Redis polling interval in seconds (default: 30)
- **--redis-sync-cert-path**: TLS certificate for gRPC service
- **--redis-sync-key-path**: TLS private key for gRPC service
- **--redis-sync-socket-path**: Unix socket path (alternative to TCP)

## Benefits of Standalone Mode

### Scalability
- **Multiple flagd instances**: One Redis sync service can serve many flagd instances
- **Resource efficiency**: Reduces Redis connections and polling load
- **Horizontal scaling**: Add more flagd instances without increasing Redis load

### Reliability
- **Isolation**: Redis sync failures don't crash flag evaluation services
- **Independent scaling**: Scale Redis sync and flag evaluation independently
- **Graceful degradation**: Flagd continues serving cached flags if Redis sync is down

### Operational Benefits
- **Centralized monitoring**: Monitor Redis sync separately from flag evaluation
- **Independent updates**: Update Redis sync logic without restarting flagd
- **Network optimization**: Redis sync service can be co-located with Redis

## Error Handling & Resilience
- **Connection testing**: PING command during initialization
- **Fallback mechanisms**: JSON module → String GET operations
- **Resync capability**: Full resynchronization on demand
- **Efficient updates**: SHA-based change detection prevents unnecessary processing
- **Automatic reconnection**: gRPC clients automatically reconnect with backoff

## Data Storage Options
- **Redis JSON Module**: Preferred method using `JSON.GET` commands
- **String storage**: Fallback using regular `GET` commands for JSON strings
- **Format conversion**: Automatic conversion to standardized JSON format
