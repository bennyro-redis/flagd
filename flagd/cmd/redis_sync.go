package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-feature/flagd/core/pkg/logger"
	redissync "github.com/open-feature/flagd/flagd/pkg/service/redis-sync"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	redisURIFlagName            = "redis-uri"
	redisIntervalFlagName       = "redis-interval"
	redisSyncPortFlagName       = "redis-sync-port"
	redisSyncCertPathFlagName   = "redis-sync-cert-path"
	redisSyncKeyPathFlagName    = "redis-sync-key-path"
	redisSyncSocketPathFlagName = "redis-sync-socket-path"
	redisLogFormatFlagName      = "redis-log-format"
)

var redisSyncCmd = &cobra.Command{
	Use:   "redis-sync",
	Short: "Start a standalone Redis sync service",
	Long: `Start a standalone Redis sync service that polls Redis for flag configurations
and exposes them via gRPC sync service. This allows flagd instances to use Redis
as a sync source through the gRPC sync provider.

Example:
  flagd redis-sync --redis-uri="redis://localhost:6379/0?key=flags" --redis-sync-port=8016

This will:
1. Poll Redis every 30 seconds (configurable) for flag configurations
2. Expose a gRPC sync service on port 8016
3. Allow flagd instances to connect via: --sources='[{"uri":"localhost:8016","provider":"grpc"}]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startRedisSyncService()
	},
}

func init() {
	flags := redisSyncCmd.Flags()

	// Redis connection flags
	flags.String(redisURIFlagName, "", "Redis URI (e.g., redis://localhost:6379/0?key=flags)")
	flags.Uint32(redisIntervalFlagName, 30, "Redis polling interval in seconds")

	// gRPC sync service flags
	flags.Uint16(redisSyncPortFlagName, 8016, "Port for the gRPC sync service")
	flags.String(redisSyncCertPathFlagName, "", "Path to TLS certificate for gRPC sync service")
	flags.String(redisSyncKeyPathFlagName, "", "Path to TLS private key for gRPC sync service")
	flags.String(redisSyncSocketPathFlagName, "", "Unix socket path for gRPC sync service")

	// Logging flags
	flags.String(redisLogFormatFlagName, "console", "Log format (console or json)")

	// Bind flags to viper
	_ = viper.BindPFlag(redisURIFlagName, flags.Lookup(redisURIFlagName))
	_ = viper.BindPFlag(redisIntervalFlagName, flags.Lookup(redisIntervalFlagName))
	_ = viper.BindPFlag(redisSyncPortFlagName, flags.Lookup(redisSyncPortFlagName))
	_ = viper.BindPFlag(redisSyncCertPathFlagName, flags.Lookup(redisSyncCertPathFlagName))
	_ = viper.BindPFlag(redisSyncKeyPathFlagName, flags.Lookup(redisSyncKeyPathFlagName))
	_ = viper.BindPFlag(redisSyncSocketPathFlagName, flags.Lookup(redisSyncSocketPathFlagName))
	_ = viper.BindPFlag(redisLogFormatFlagName, flags.Lookup(redisLogFormatFlagName))

	// Mark required flags
	_ = redisSyncCmd.MarkFlagRequired(redisURIFlagName)

	rootCmd.AddCommand(redisSyncCmd)
}

func startRedisSyncService() error {
	// Setup logger
	logLevel := zapcore.InfoLevel
	logFormat := viper.GetString(redisLogFormatFlagName)

	var zapConfig zap.Config
	if logFormat == "json" {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zapConfig.Level = zap.NewAtomicLevelAt(logLevel)

	zapLogger, err := zapConfig.Build()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer zapLogger.Sync()

	log := logger.NewLogger(zapLogger, false)

	// Get configuration
	redisURI := viper.GetString(redisURIFlagName)
	redisInterval := viper.GetUint32(redisIntervalFlagName)
	syncPort := viper.GetUint16(redisSyncPortFlagName)
	certPath := viper.GetString(redisSyncCertPathFlagName)
	keyPath := viper.GetString(redisSyncKeyPathFlagName)
	socketPath := viper.GetString(redisSyncSocketPathFlagName)

	log.Info(fmt.Sprintf("Starting Redis sync service with URI: %s", redisURI))
	log.Info(fmt.Sprintf("Redis polling interval: %d seconds", redisInterval))
	log.Info(fmt.Sprintf("gRPC sync service port: %d", syncPort))

	// Create Redis sync service
	service, err := redissync.NewService(redissync.Config{
		RedisURI:      redisURI,
		RedisInterval: redisInterval,
		SyncPort:      syncPort,
		CertPath:      certPath,
		KeyPath:       keyPath,
		SocketPath:    socketPath,
		Logger:        log,
	})
	if err != nil {
		return fmt.Errorf("failed to create Redis sync service: %w", err)
	}

	// Setup context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start the service
	return service.Start(ctx)
}
