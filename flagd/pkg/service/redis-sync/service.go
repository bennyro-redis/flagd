package redissync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/open-feature/flagd/core/pkg/evaluator"
	"github.com/open-feature/flagd/core/pkg/logger"
	"github.com/open-feature/flagd/core/pkg/model"
	"github.com/open-feature/flagd/core/pkg/store"
	coresync "github.com/open-feature/flagd/core/pkg/sync"
	"github.com/open-feature/flagd/core/pkg/sync/redis"
	flagsync "github.com/open-feature/flagd/flagd/pkg/service/flag-sync"
	"golang.org/x/sync/errgroup"
)

// Service represents a standalone Redis sync service that exposes flags via gRPC
type Service struct {
	redisSync   *redis.Sync
	flagStore   *store.Store
	syncService *flagsync.Service
	evaluator   evaluator.IEvaluator
	logger      *logger.Logger
	mu          sync.RWMutex
}

// Config holds configuration for the Redis sync service
type Config struct {
	RedisURI     string
	RedisInterval uint32
	SyncPort     uint16
	CertPath     string
	KeyPath      string
	SocketPath   string
	Logger       *logger.Logger
}

// NewService creates a new Redis sync service
func NewService(cfg Config) (*Service, error) {
	// Create Redis sync provider
	redisSync, err := redis.NewRedisSync(cfg.RedisURI, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis sync provider: %w", err)
	}
	redisSync.SetInterval(cfg.RedisInterval)

	// Create store for flag data
	flagStore, err := store.NewStore(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create flag store: %w", err)
	}

	// Create evaluator for parsing flag data
	eval := evaluator.NewJSON(cfg.Logger, flagStore)

	// Create gRPC sync service
	syncService, err := flagsync.NewSyncService(flagsync.SvcConfigurations{
		Logger:     cfg.Logger,
		Port:       cfg.SyncPort,
		Sources:    []string{cfg.RedisURI}, // Track Redis as source
		Store:      flagStore,
		CertPath:   cfg.CertPath,
		KeyPath:    cfg.KeyPath,
		SocketPath: cfg.SocketPath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sync service: %w", err)
	}

	return &Service{
		redisSync:   redisSync,
		flagStore:   flagStore,
		syncService: syncService,
		evaluator:   eval,
		logger:      cfg.Logger,
	}, nil
}

// Start starts the Redis sync service
func (s *Service) Start(ctx context.Context) error {
	s.logger.Info("Starting Redis sync service...")

	// Create error group for managing goroutines
	g, gCtx := errgroup.WithContext(ctx)

	// Channel for sync data
	dataSync := make(chan coresync.DataSync, 1)

	// Initialize Redis sync provider
	if err := s.redisSync.Init(gCtx); err != nil {
		return fmt.Errorf("failed to initialize Redis sync provider: %w", err)
	}

	// Start Redis sync provider
	g.Go(func() error {
		s.logger.Info("Starting Redis sync provider...")
		if err := s.redisSync.Sync(gCtx, dataSync); err != nil {
			return fmt.Errorf("Redis sync error: %w", err)
		}
		return nil
	})

	// Start gRPC sync service
	g.Go(func() error {
		s.logger.Info("Starting gRPC sync service...")
		if err := s.syncService.Start(gCtx); err != nil {
			return fmt.Errorf("sync service error: %w", err)
		}
		return nil
	})

	// Process sync data updates
	g.Go(func() error {
		return s.processSyncData(gCtx, dataSync)
	})

	s.logger.Info("Redis sync service started successfully")

	// Wait for all goroutines to complete or context cancellation
	if err := g.Wait(); err != nil {
		return fmt.Errorf("Redis sync service error: %w", err)
	}

	return nil
}

// processSyncData handles incoming sync data from Redis and updates the store
func (s *Service) processSyncData(ctx context.Context, dataSync <-chan coresync.DataSync) error {
	for {
		select {
		case data := <-dataSync:
			s.logger.Debug(fmt.Sprintf("Received flag data from Redis: %s", data.Source))
			
			if err := s.updateStoreFromSyncData(data); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to update store: %v", err))
				continue
			}
			
			// Emit changes to sync service subscribers
			s.syncService.Emit(false, data.Source)
			
		case <-ctx.Done():
			s.logger.Info("Stopping sync data processor...")
			return nil
		}
	}
}

// updateStoreFromSyncData parses flag data and updates the store
func (s *Service) updateStoreFromSyncData(data coresync.DataSync) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if data.FlagData == "" {
		s.logger.Debug("Received empty flag data, skipping update")
		return nil
	}

	s.logger.Debug(fmt.Sprintf("Updating store with %d bytes of flag data", len(data.FlagData)))

	// Use the evaluator to parse and update the store
	// The evaluator's SetState method handles JSON parsing and store updates
	notifications, resyncRequired, err := s.evaluator.SetState(data)
	if err != nil {
		return fmt.Errorf("failed to update evaluator state: %w", err)
	}

	s.logger.Debug(fmt.Sprintf("Store updated successfully, %d flags changed, resync required: %v", 
		len(notifications), resyncRequired))

	// If resync is required, trigger a full resync
	if resyncRequired {
		s.logger.Info("Resync required, triggering full resync...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			
			if err := s.redisSync.ReSync(ctx, make(chan coresync.DataSync, 1)); err != nil {
				s.logger.Error(fmt.Sprintf("Resync failed: %v", err))
			}
		}()
	}

	return nil
}

// GetFlagConfiguration returns the current flag configuration as JSON
func (s *Service) GetFlagConfiguration() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get all flags from store
	flags, metadata, err := s.flagStore.GetAll(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get flags from store: %w", err)
	}

	// Create flag configuration structure
	config := struct {
		Flags    map[string]model.Flag `json:"flags"`
		Metadata model.Metadata        `json:"$evaluators,omitempty"`
	}{
		Flags:    flags,
		Metadata: metadata,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal flag configuration: %w", err)
	}

	return string(jsonData), nil
}

// IsReady returns true if the service is ready to serve requests
func (s *Service) IsReady() bool {
	return s.redisSync.IsReady()
}

// Shutdown gracefully shuts down the service
func (s *Service) Shutdown() {
	s.logger.Info("Shutting down Redis sync service...")
	
	// The sync service and Redis sync provider will be stopped
	// when the context is cancelled in the Start method
}
