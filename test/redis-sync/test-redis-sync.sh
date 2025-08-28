#!/bin/bash

# Test script for standalone Redis sync service
# This script demonstrates how to use the new Redis sync functionality

set -e

echo "🚀 Testing Standalone Redis Sync Service"
echo "========================================"

# Check if Redis is running
if ! command -v redis-cli &> /dev/null; then
    echo "❌ Redis CLI not found. Please install Redis."
    exit 1
fi

# Check if Redis server is running
if ! redis-cli ping &> /dev/null; then
    echo "❌ Redis server is not running. Please start Redis server."
    echo "   You can start it with: redis-server"
    exit 1
fi

echo "✅ Redis server is running"

# Build flagd if not already built
if [ ! -f "./flagd-test" ]; then
    echo "🔨 Building flagd..."
    go build -o flagd-test ./main.go
fi

echo "✅ Flagd built successfully"

# Prepare test flag configuration
echo "📝 Setting up test flag configuration in Redis..."

# Create test flag configuration
cat > /tmp/test-flags.json << 'EOF'
{
  "flags": {
    "welcome-message": {
      "state": "ENABLED",
      "variants": {
        "on": "Welcome to our application!",
        "off": "Application is under maintenance"
      },
      "defaultVariant": "on"
    },
    "feature-x-enabled": {
      "state": "ENABLED",
      "variants": {
        "on": true,
        "off": false
      },
      "defaultVariant": "off",
      "targeting": {
        "if": [
          {
            "in": ["beta", {"var": "user.groups"}]
          },
          "on",
          "off"
        ]
      }
    }
  },
  "$evaluators": {
    "fractional": 0.1
  }
}
EOF

# Store flag configuration in Redis
redis-cli set flags "$(cat /tmp/test-flags.json)" > /dev/null
echo "✅ Test flags stored in Redis"

# Test 1: Start Redis sync service in background
echo ""
echo "🧪 Test 1: Starting Redis sync service..."
./flagd-test redis-sync \
  --redis-uri="redis://localhost:6379/0?key=flags" \
  --redis-sync-port=8016 \
  --redis-interval=10 \
  --redis-log-format=console &

REDIS_SYNC_PID=$!
echo "✅ Redis sync service started (PID: $REDIS_SYNC_PID)"

# Wait a moment for the service to start
sleep 3

# Test 2: Check if gRPC sync service is listening
echo ""
echo "🧪 Test 2: Checking if gRPC sync service is listening on port 8016..."
if command -v netstat &> /dev/null; then
    if netstat -ln | grep -q ":8016"; then
        echo "✅ gRPC sync service is listening on port 8016"
    else
        echo "❌ gRPC sync service is not listening on port 8016"
    fi
elif command -v ss &> /dev/null; then
    if ss -ln | grep -q ":8016"; then
        echo "✅ gRPC sync service is listening on port 8016"
    else
        echo "❌ gRPC sync service is not listening on port 8016"
    fi
else
    echo "⚠️  Cannot check port status (netstat/ss not available)"
fi

# Test 3: Start flagd using the Redis sync service
echo ""
echo "🧪 Test 3: Starting flagd with gRPC sync source..."
./flagd-test start \
  --sources='[{"uri":"localhost:8016","provider":"grpc"}]' \
  --port=8013 \
  --sync-port=8015 &

FLAGD_PID=$!
echo "✅ Flagd started (PID: $FLAGD_PID)"

# Wait for flagd to start and connect
sleep 5

# Test 4: Check if flagd is ready
echo ""
echo "🧪 Test 4: Checking if flagd is ready..."
if command -v curl &> /dev/null; then
    if curl -s http://localhost:8014/readyz | grep -q "OK"; then
        echo "✅ Flagd is ready and serving requests"
    else
        echo "❌ Flagd is not ready"
    fi
else
    echo "⚠️  Cannot check flagd readiness (curl not available)"
fi

# Test 5: Update flags in Redis and verify propagation
echo ""
echo "🧪 Test 5: Testing flag updates..."
echo "📝 Updating flags in Redis..."

# Update flag configuration
cat > /tmp/updated-flags.json << 'EOF'
{
  "flags": {
    "welcome-message": {
      "state": "ENABLED",
      "variants": {
        "on": "Welcome to our UPDATED application!",
        "off": "Application is under maintenance"
      },
      "defaultVariant": "on"
    },
    "feature-x-enabled": {
      "state": "ENABLED",
      "variants": {
        "on": true,
        "off": false
      },
      "defaultVariant": "on"
    },
    "new-feature": {
      "state": "ENABLED",
      "variants": {
        "enabled": true,
        "disabled": false
      },
      "defaultVariant": "enabled"
    }
  },
  "$evaluators": {
    "fractional": 0.1
  }
}
EOF

redis-cli set flags "$(cat /tmp/updated-flags.json)" > /dev/null
echo "✅ Flags updated in Redis"

echo "⏳ Waiting for sync (polling interval is 10 seconds)..."
sleep 12

echo ""
echo "🎉 Test completed!"
echo ""
echo "📊 Summary:"
echo "  - Redis sync service running on port 8016"
echo "  - Flagd running on port 8013 (gRPC) and 8014 (HTTP)"
echo "  - Flagd sync stream on port 8015"
echo "  - Flags are being synced from Redis every 10 seconds"
echo ""
echo "🔗 You can now test flag evaluation:"
echo "  curl http://localhost:8014/ofrep/v1/evaluate/flags/welcome-message"
echo ""
echo "🛑 To stop the services:"
echo "  kill $REDIS_SYNC_PID $FLAGD_PID"

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up..."
    kill $REDIS_SYNC_PID $FLAGD_PID 2>/dev/null || true
    rm -f /tmp/test-flags.json /tmp/updated-flags.json
    echo "✅ Cleanup completed"
}

# Set trap for cleanup on script exit
trap cleanup EXIT

# Keep the script running to show logs
echo ""
echo "📋 Showing logs (press Ctrl+C to stop)..."
wait
