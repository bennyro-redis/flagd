package redis

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/open-feature/flagd/core/pkg/logger"
	"github.com/open-feature/flagd/core/pkg/sync"
	"github.com/open-feature/flagd/core/pkg/utils"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron"
	"golang.org/x/crypto/sha3"
)

// Sync implements the ISync interface for Redis JSON documents
type Sync struct {
	URI      string
	Client   RedisClient
	Cron     Cron
	Logger   *logger.Logger
	Key      string
	Database int
	Password string
	TLS      bool
	Interval uint32
	LastSHA  string
	ready    bool
}

// RedisClient defines the interface for Redis operations
type RedisClient interface {
	JSONGet(ctx context.Context, key string, path ...string) *redis.JSONCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// Cron defines the interface for cron operations
type Cron interface {
	AddFunc(spec string, cmd func()) error
	Start()
	Stop()
}

// NewRedisSync creates a new Redis sync provider
func NewRedisSync(uri string, logger *logger.Logger) (*Sync, error) {
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URI: %w", err)
	}

	if parsedURI.Scheme != "redis" && parsedURI.Scheme != "rediss" {
		return nil, fmt.Errorf("unsupported scheme: %s, expected redis or rediss", parsedURI.Scheme)
	}

	// Extract connection parameters
	host := parsedURI.Host
	if host == "" {
		host = "localhost:6379"
	}

	// Extract database number from path
	database := 0
	if parsedURI.Path != "" && parsedURI.Path != "/" {
		dbStr := strings.TrimPrefix(parsedURI.Path, "/")
		if db, err := strconv.Atoi(dbStr); err == nil {
			database = db
		}
	}

	// Extract password from user info
	password := ""
	if parsedURI.User != nil {
		password, _ = parsedURI.User.Password()
	}

	// Extract key from query parameters
	key := parsedURI.Query().Get("key")
	if key == "" {
		return nil, errors.New("Redis key must be specified in query parameter 'key'")
	}

	// Check for TLS
	useTLS := parsedURI.Scheme == "rediss"

	// Create Redis client options
	opts := &redis.Options{
		Addr:     host,
		Password: password,
		DB:       database,
	}

	if useTLS {
		opts.TLSConfig = &tls.Config{
			ServerName: strings.Split(host, ":")[0],
		}
	}

	client := redis.NewClient(opts)

	return &Sync{
		URI:      uri,
		Client:   client,
		Cron:     cron.New(),
		Logger:   logger,
		Key:      key,
		Database: database,
		Password: password,
		TLS:      useTLS,
		Interval: 30, // Default to 30 seconds
	}, nil
}

// Init initializes the Redis sync provider
func (rs *Sync) Init(ctx context.Context) error {
	// Test connection
	if err := rs.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rs.Logger.Info(fmt.Sprintf("Redis sync provider initialized for key: %s", rs.Key))
	return nil
}

// Sync starts the synchronization process
func (rs *Sync) Sync(ctx context.Context, dataSync chan<- sync.DataSync) error {
	rs.Logger.Info(fmt.Sprintf("starting Redis sync for key %s with interval %ds", rs.Key, rs.Interval))

	// Add cron job for periodic polling
	_ = rs.Cron.AddFunc(fmt.Sprintf("*/%d * * * *", rs.Interval), func() {
		rs.Logger.Debug(fmt.Sprintf("fetching configuration from Redis key: %s", rs.Key))
		previousSHA := rs.LastSHA
		data, err := rs.fetchData(ctx)
		if err != nil {
			rs.Logger.Error(fmt.Sprintf("error fetching from Redis: %s", err.Error()))
			return
		}

		if data == "" {
			rs.Logger.Debug("Redis key not found or empty")
			return
		}

		if previousSHA == "" {
			rs.Logger.Debug("configuration created")
			dataSync <- sync.DataSync{FlagData: data, Source: rs.URI}
		} else if previousSHA != rs.LastSHA {
			rs.Logger.Debug("configuration updated")
			dataSync <- sync.DataSync{FlagData: data, Source: rs.URI}
		}
	})

	// Initial fetch
	rs.Logger.Debug(fmt.Sprintf("initial sync of Redis key: %s", rs.Key))
	data, err := rs.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("initial Redis fetch failed: %w", err)
	}

	if data != "" {
		dataSync <- sync.DataSync{FlagData: data, Source: rs.URI}
	}

	rs.ready = true
	rs.Cron.Start()

	// Wait for context cancellation
	<-ctx.Done()
	rs.Cron.Stop()

	return nil
}

// ReSync performs a full resynchronization
func (rs *Sync) ReSync(ctx context.Context, dataSync chan<- sync.DataSync) error {
	data, err := rs.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("Redis resync failed: %w", err)
	}

	if data != "" {
		dataSync <- sync.DataSync{FlagData: data, Source: rs.URI}
	}

	return nil
}

// IsReady returns true if the provider is ready
func (rs *Sync) IsReady() bool {
	return rs.ready
}

// fetchData retrieves and processes data from Redis
func (rs *Sync) fetchData(ctx context.Context) (string, error) {
	// Try JSON.GET first (Redis JSON module)
	jsonResult := rs.Client.JSONGet(ctx, rs.Key, ".")
	if jsonResult.Err() == nil {
		// Successfully used Redis JSON module
		var jsonData interface{}
		var err error
		jsonData, err = jsonResult.Result()
		if err != nil {
			return "", fmt.Errorf("failed to get result from Redis JSON command: %w", err)
		}

		if jsonData == nil {
			return "", nil
		}

		// Convert the result to string
		jsonString, ok := jsonData.(string)
		if !ok {
			return "", fmt.Errorf("unexpected data type from Redis JSON.GET: %T", jsonData)
		}

		if jsonString == "" {
			return "", nil
		}

		// Convert to standard JSON format if needed
		convertedJSON, err := utils.ConvertToJSON([]byte(jsonString), ".json", "application/json")
		if err != nil {
			return "", fmt.Errorf("error converting Redis JSON to standard format: %w", err)
		}

		// Generate SHA for change detection
		if convertedJSON != "" {
			rs.LastSHA = rs.generateSHA([]byte(convertedJSON))
		}

		return convertedJSON, nil
	}

	// Fallback to regular GET if JSON module is not available or key doesn't exist
	if jsonResult.Err() != redis.Nil {
		rs.Logger.Debug(fmt.Sprintf("Redis JSON.GET failed, falling back to GET: %v", jsonResult.Err()))
	}

	// Use GET to retrieve the JSON document stored as a string
	result := rs.Client.Get(ctx, rs.Key)
	if err := result.Err(); err != nil {
		if err == redis.Nil {
			// Key doesn't exist
			return "", nil
		}
		return "", fmt.Errorf("failed to get data from Redis: %w", err)
	}

	jsonString := result.Val()
	if jsonString == "" {
		return "", nil
	}

	// Convert to standard JSON format if needed
	convertedJSON, err := utils.ConvertToJSON([]byte(jsonString), ".json", "application/json")
	if err != nil {
		return "", fmt.Errorf("error converting Redis data to standard JSON format: %w", err)
	}

	// Generate SHA for change detection
	if convertedJSON != "" {
		rs.LastSHA = rs.generateSHA([]byte(convertedJSON))
	}

	return convertedJSON, nil
}

// generateSHA generates a SHA hash for change detection
func (rs *Sync) generateSHA(data []byte) string {
	hasher := sha3.New256()
	hasher.Write(data)
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

// SetInterval sets the polling interval
func (rs *Sync) SetInterval(interval uint32) {
	rs.Interval = interval
}

// NewRedisSyncFromConfig creates a new Redis sync provider from SourceConfig
func NewRedisSyncFromConfig(config sync.SourceConfig, logger *logger.Logger) (*Sync, error) {
	rs, err := NewRedisSync(config.URI, logger)
	if err != nil {
		return nil, err
	}

	// Override interval if specified in config
	if config.Interval > 0 {
		rs.SetInterval(config.Interval)
	}

	// Override TLS setting if specified in config
	if config.TLS {
		rs.TLS = true
	}

	return rs, nil
}

// Close closes the Redis connection
func (rs *Sync) Close() error {
	if rs.Client != nil {
		return rs.Client.Close()
	}
	return nil
}
