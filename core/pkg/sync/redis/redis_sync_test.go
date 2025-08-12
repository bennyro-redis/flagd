package redis

import (
	"context"
	"errors"
	"testing"

	"github.com/open-feature/flagd/core/pkg/logger"
	"github.com/open-feature/flagd/core/pkg/sync"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRedisClient implements the RedisClient interface for testing
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) JSONGet(ctx context.Context, key string, path ...string) *redis.JSONCmd {
	args := m.Called(ctx, key, path)
	return args.Get(0).(*redis.JSONCmd)
}

func (m *MockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringCmd)
}

func (m *MockRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	args := m.Called(ctx)
	return args.Get(0).(*redis.StatusCmd)
}

func (m *MockRedisClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockCron implements the Cron interface for testing
type MockCron struct {
	mock.Mock
	funcs []func()
}

func (m *MockCron) AddFunc(spec string, cmd func()) error {
	m.funcs = append(m.funcs, cmd)
	args := m.Called(spec, cmd)
	return args.Error(0)
}

func (m *MockCron) Start() {
	m.Called()
}

func (m *MockCron) Stop() {
	m.Called()
}

func (m *MockCron) TriggerFunc(index int) {
	if index < len(m.funcs) {
		m.funcs[index]()
	}
}

func TestNewRedisSync(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expectError bool
		expectedKey string
		expectedDB  int
	}{
		{
			name:        "valid redis URI with key",
			uri:         "redis://localhost:6379/0?key=flags",
			expectError: false,
			expectedKey: "flags",
			expectedDB:  0,
		},
		{
			name:        "valid rediss URI with key and auth",
			uri:         "rediss://user:pass@localhost:6380/1?key=feature-flags",
			expectError: false,
			expectedKey: "feature-flags",
			expectedDB:  1,
		},
		{
			name:        "invalid scheme",
			uri:         "http://localhost:6379?key=flags",
			expectError: true,
		},
		{
			name:        "missing key parameter",
			uri:         "redis://localhost:6379/0",
			expectError: true,
		},
		{
			name:        "invalid URI",
			uri:         "not-a-uri",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logger.NewLogger(zap.NewNop(), false)
			sync, err := NewRedisSync(tt.uri, logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, sync)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sync)
				assert.Equal(t, tt.expectedKey, sync.Key)
				assert.Equal(t, tt.expectedDB, sync.Database)
			}
		})
	}
}

func TestRedisSync_Init(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockRedisClient)
		expectError bool
	}{
		{
			name: "successful initialization",
			setupMock: func(m *MockRedisClient) {
				statusCmd := redis.NewStatusCmd(context.Background())
				statusCmd.SetVal("PONG")
				m.On("Ping", mock.Anything).Return(statusCmd)
			},
			expectError: false,
		},
		{
			name: "connection failure",
			setupMock: func(m *MockRedisClient) {
				statusCmd := redis.NewStatusCmd(context.Background())
				statusCmd.SetErr(errors.New("connection failed"))
				m.On("Ping", mock.Anything).Return(statusCmd)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockRedisClient{}
			tt.setupMock(mockClient)

			logger := logger.NewLogger(zap.NewNop(), false)
			rs := &Sync{
				Client: mockClient,
				Logger: logger,
				Key:    "test-key",
			}

			err := rs.Init(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestRedisSync_fetchData(t *testing.T) {
	tests := []struct {
		name         string
		setupMock    func(*MockRedisClient)
		expectedData string
		expectError  bool
	}{
		{
			name: "successful fetch with JSON module",
			setupMock: func(m *MockRedisClient) {
				jsonCmd := &redis.JSONCmd{}
				jsonCmd.SetVal(`{"flags":{"test":{"state":"ENABLED","variants":{"on":true},"defaultVariant":"on"}}}`)
				m.On("JSONGet", mock.Anything, "test-key", mock.Anything).Return(jsonCmd)
			},
			expectedData: `{"flags":{"test":{"state":"ENABLED","variants":{"on":true},"defaultVariant":"on"}}}`,
			expectError:  false,
		},
		{
			name: "fallback to GET when JSON module unavailable",
			setupMock: func(m *MockRedisClient) {
				// JSON.GET fails (module not available)
				jsonCmd := &redis.JSONCmd{}
				jsonCmd.SetErr(errors.New("unknown command 'JSON.GET'"))
				m.On("JSONGet", mock.Anything, "test-key", mock.Anything).Return(jsonCmd)

				// Fallback to regular GET
				stringCmd := redis.NewStringCmd(context.Background())
				stringCmd.SetVal(`{"flags":{"test":{"state":"ENABLED","variants":{"on":true},"defaultVariant":"on"}}}`)
				m.On("Get", mock.Anything, "test-key").Return(stringCmd)
			},
			expectedData: `{"flags":{"test":{"state":"ENABLED","variants":{"on":true},"defaultVariant":"on"}}}`,
			expectError:  false,
		},
		{
			name: "key not found with JSON module",
			setupMock: func(m *MockRedisClient) {
				jsonCmd := &redis.JSONCmd{}
				jsonCmd.SetErr(redis.Nil)
				m.On("JSONGet", mock.Anything, "test-key", mock.Anything).Return(jsonCmd)

				// Fallback to regular GET
				stringCmd := redis.NewStringCmd(context.Background())
				stringCmd.SetErr(redis.Nil)
				m.On("Get", mock.Anything, "test-key").Return(stringCmd)
			},
			expectedData: "",
			expectError:  false,
		},
		{
			name: "redis error on both JSON and GET",
			setupMock: func(m *MockRedisClient) {
				jsonCmd := &redis.JSONCmd{}
				jsonCmd.SetErr(errors.New("connection error"))
				m.On("JSONGet", mock.Anything, "test-key", mock.Anything).Return(jsonCmd)

				stringCmd := redis.NewStringCmd(context.Background())
				stringCmd.SetErr(errors.New("connection error"))
				m.On("Get", mock.Anything, "test-key").Return(stringCmd)
			},
			expectedData: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockRedisClient{}
			tt.setupMock(mockClient)

			logger := logger.NewLogger(zap.NewNop(), false)
			rs := &Sync{
				Client: mockClient,
				Logger: logger,
				Key:    "test-key",
			}

			data, err := rs.fetchData(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestRedisSync_ReSync(t *testing.T) {
	mockClient := &MockRedisClient{}
	jsonCmd := &redis.JSONCmd{}
	jsonCmd.SetVal(`{"flags":{"test":{"state":"ENABLED"}}}`)
	mockClient.On("JSONGet", mock.Anything, "test-key", mock.Anything).Return(jsonCmd)

	logger := logger.NewLogger(zap.NewNop(), false)
	rs := &Sync{
		Client: mockClient,
		Logger: logger,
		Key:    "test-key",
		URI:    "redis://localhost:6379?key=test-key",
	}

	dataSync := make(chan sync.DataSync, 1)
	err := rs.ReSync(context.Background(), dataSync)

	assert.NoError(t, err)
	assert.Len(t, dataSync, 1)

	syncData := <-dataSync
	assert.Equal(t, `{"flags":{"test":{"state":"ENABLED"}}}`, syncData.FlagData)
	assert.Equal(t, "redis://localhost:6379?key=test-key", syncData.Source)

	mockClient.AssertExpectations(t)
}

func TestRedisSync_IsReady(t *testing.T) {
	rs := &Sync{}
	assert.False(t, rs.IsReady())

	rs.ready = true
	assert.True(t, rs.IsReady())
}

func TestRedisSync_SetInterval(t *testing.T) {
	rs := &Sync{Interval: 30}
	rs.SetInterval(60)
	assert.Equal(t, uint32(60), rs.Interval)
}

func TestRedisSync_Close(t *testing.T) {
	mockClient := &MockRedisClient{}
	mockClient.On("Close").Return(nil)

	rs := &Sync{Client: mockClient}
	err := rs.Close()

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}
