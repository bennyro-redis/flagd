# Redis Sync Provider Setup

This guide explains how to set up and use the Redis sync provider with flagd.

## Prerequisites

- Redis server (version 6.0 or later recommended)
- Redis JSON module (optional but recommended for better performance)
- flagd binary or container

## Redis JSON Module

The Redis sync provider supports both:
1. **Redis JSON module** (recommended) - Uses `JSON.GET` for native JSON operations
2. **Regular Redis strings** (fallback) - Uses `GET` for string-based storage

The provider automatically detects if the Redis JSON module is available and falls back to regular string operations if needed.

## Quick Start

### 1. Start Redis

#### Option A: Redis with JSON module (recommended)
```bash
# Using Redis Stack (includes JSON module)
docker run -d --name redis -p 6379:6379 redis/redis-stack:latest

# Or using RedisJSON module
docker run -d --name redis -p 6379:6379 redislabs/rejson:latest
```

#### Option B: Regular Redis (fallback mode)
```bash
docker run -d --name redis -p 6379:6379 redis:latest
```

Or install Redis locally following the [official Redis installation guide](https://redis.io/docs/getting-started/installation/).

### 2. Store Flag Configuration in Redis

#### Option A: Using Redis JSON module (if available)
```bash
# Set JSON document using JSON.SET
redis-cli JSON.SET flags . '{"flags":{"myBoolFlag":{"state":"ENABLED","variants":{"on":true,"off":false},"defaultVariant":"on"}}}'

# Or using a file
redis-cli JSON.SET flags . "$(cat samples/example_flags_redis.json)"

# Update individual flags
redis-cli JSON.SET flags .flags.myBoolFlag.state '"DISABLED"'
```

#### Option B: Using regular Redis strings (fallback)
```bash
# Using redis-cli
redis-cli SET flags '{"flags":{"myBoolFlag":{"state":"ENABLED","variants":{"on":true,"off":false},"defaultVariant":"on"}}}'

# Or using a file
redis-cli SET flags "$(cat samples/example_flags_redis.json)"
```

### 3. Start flagd with Redis Sync

```bash
flagd start --uri redis://localhost:6379/0?key=flags
```

## Configuration Options

### URI Format

```
redis://[username:password@]host:port[/database]?key=redis_key[&param=value]
```

- **Scheme**: `redis://` for plain connections, `rediss://` for TLS
- **Authentication**: Optional username:password
- **Host/Port**: Redis server address (default: localhost:6379)
- **Database**: Redis database number (default: 0)
- **Key**: Required query parameter specifying the Redis key containing flags

### Examples

Basic connection:
```bash
flagd start --uri redis://localhost:6379?key=flags
```

With authentication:
```bash
flagd start --uri redis://user:pass@localhost:6379?key=flags
```

With TLS:
```bash
flagd start --uri rediss://localhost:6380?key=flags
```

Specific database:
```bash
flagd start --uri redis://localhost:6379/2?key=my-flags
```

### Configuration File

You can also use a configuration file:

```yaml
sources:
  - uri: redis://localhost:6379/0?key=flags
    provider: redis
    interval: 30  # Poll every 30 seconds
    tls: false    # Override TLS setting
```

## Flag Format

Flags should be stored as a JSON string in Redis following the flagd schema:

```json
{
  "$schema": "https://flagd.dev/schema/v0/flags.json",
  "flags": {
    "myFlag": {
      "state": "ENABLED",
      "variants": {
        "on": true,
        "off": false
      },
      "defaultVariant": "on"
    }
  }
}
```

## Updating Flags

### With Redis JSON module (recommended)
```bash
# Update entire flag configuration
redis-cli JSON.SET flags . '{"flags":{"myFlag":{"state":"DISABLED","variants":{"on":true,"off":false},"defaultVariant":"off"}}}'

# Update individual flag properties
redis-cli JSON.SET flags .flags.myFlag.state '"DISABLED"'
redis-cli JSON.SET flags .flags.myFlag.defaultVariant '"off"'

# Add new flags
redis-cli JSON.SET flags .flags.newFlag '{"state":"ENABLED","variants":{"yes":true,"no":false},"defaultVariant":"yes"}'
```

### With regular Redis strings (fallback)
```bash
# Update entire flag configuration
redis-cli SET flags '{"flags":{"myFlag":{"state":"DISABLED","variants":{"on":true,"off":false},"defaultVariant":"off"}}}'
```

flagd will detect the change and update the flag configuration automatically based on the polling interval.

## Redis JSON Module Benefits

When using Redis with the JSON module, you get several advantages:

1. **Native JSON Operations**: Direct manipulation of JSON documents without string parsing
2. **Atomic Updates**: Update individual flag properties without affecting others
3. **Better Performance**: More efficient than string-based operations for complex JSON
4. **Type Safety**: JSON module validates JSON structure automatically
5. **Partial Updates**: Modify specific paths within the JSON document

### Example: Gradual Flag Rollout with JSON Module
```bash
# Start with 10% rollout
redis-cli JSON.SET flags .flags.newFeature.targeting.fractional '[{"var":"userId"},[["enabled",10],["disabled",90]]]'

# Increase to 50%
redis-cli JSON.SET flags .flags.newFeature.targeting.fractional '[{"var":"userId"},[["enabled",50],["disabled",50]]]'

# Full rollout
redis-cli JSON.SET flags .flags.newFeature.defaultVariant '"enabled"'
redis-cli JSON.DEL flags .flags.newFeature.targeting
```

## Monitoring

You can monitor Redis sync status through flagd's metrics and logs:

- Check flagd logs for sync events
- Monitor Redis connection status
- Use flagd's health endpoints

## Troubleshooting

### Connection Issues

1. Verify Redis is running: `redis-cli ping`
2. Check network connectivity
3. Verify authentication credentials
4. Check TLS configuration for `rediss://` URIs

### Flag Not Found

1. Verify the key exists: `redis-cli EXISTS flags`
2. Check the key content: `redis-cli GET flags`
3. Validate JSON format
4. Check flagd logs for parsing errors

### Performance Considerations

- Use appropriate polling intervals (default: 30 seconds)
- Consider Redis memory usage for large flag configurations
- Monitor Redis performance metrics
- Use Redis persistence for production deployments

## Security

- Use authentication in production environments
- Enable TLS for network encryption
- Restrict Redis access using firewall rules
- Consider using Redis ACLs for fine-grained access control
