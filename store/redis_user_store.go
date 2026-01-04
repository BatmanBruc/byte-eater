package store

import (
	"fmt"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/types"
)

type RedisUserStore struct {
	client *RedisClient
	ttl    time.Duration
}

func NewRedisUserStore(redisClient *RedisClient, ttlHours int) *RedisUserStore {
	ttl := time.Duration(ttlHours) * time.Hour
	if ttlHours <= 0 {
		ttl = 24 * time.Hour
	}

	return &RedisUserStore{
		client: redisClient,
		ttl:    ttl,
	}
}

func (s *RedisUserStore) GetUserOptions(userID int64) (map[string]interface{}, error) {
	key := s.client.generateKey("user_options", fmt.Sprintf("%d", userID))
	var options map[string]interface{}
	if err := s.client.Get(key, &options); err != nil {
		return make(map[string]interface{}), nil
	}
	if options == nil {
		return make(map[string]interface{}), nil
	}
	return options, nil
}

func (s *RedisUserStore) SetUserOptions(userID int64, options map[string]interface{}) error {
	key := s.client.generateKey("user_options", fmt.Sprintf("%d", userID))
	return s.client.Set(key, options, s.ttl)
}

func (s *RedisUserStore) GetUserPending(userID int64) ([]types.PendingSelection, error) {
	key := s.client.generateKey("user_pending", fmt.Sprintf("%d", userID))
	var pending []types.PendingSelection
	if err := s.client.Get(key, &pending); err != nil {
		return []types.PendingSelection{}, nil
	}
	if pending == nil {
		return []types.PendingSelection{}, nil
	}
	return pending, nil
}

func (s *RedisUserStore) SetUserPending(userID int64, pending []types.PendingSelection) error {
	key := s.client.generateKey("user_pending", fmt.Sprintf("%d", userID))
	return s.client.Set(key, pending, s.ttl)
}
