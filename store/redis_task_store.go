package store

import (
	"fmt"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/google/uuid"
)

type RedisTaskStore struct {
	client *RedisClient
	ttl    time.Duration
}

func NewRedisTaskStore(redisClient *RedisClient, ttlHours int) *RedisTaskStore {
	ttl := time.Duration(ttlHours) * time.Hour
	if ttlHours <= 0 {
		ttl = 24 * time.Hour
	}

	return &RedisTaskStore{
		client: redisClient,
		ttl:    ttl,
	}
}

func (s *RedisTaskStore) CreateTask(task *types.Task) error {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.ExpiresAt = now.Add(s.ttl)

	taskKey := s.client.generateKey("task", task.ID)
	if err := s.client.Set(taskKey, task, s.ttl); err != nil {
		return err
	}

	userTasksKey := s.client.generateKey("user_tasks", fmt.Sprintf("%d", task.UserID))
	var taskIDs []string
	_ = s.client.Get(userTasksKey, &taskIDs)
	taskIDs = append(taskIDs, task.ID)
	if err := s.client.Set(userTasksKey, taskIDs, s.ttl); err != nil {
		s.client.Del(taskKey)
		return err
	}

	return nil
}

func (s *RedisTaskStore) GetTask(taskID string) (*types.Task, error) {
	taskKey := s.client.generateKey("task", taskID)

	var task types.Task
	if err := s.client.Get(taskKey, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

func (s *RedisTaskStore) GetUserTasks(userID int64) ([]*types.Task, error) {
	userTasksKey := s.client.generateKey("user_tasks", fmt.Sprintf("%d", userID))

	var taskIDs []string
	if err := s.client.Get(userTasksKey, &taskIDs); err != nil {
		return []*types.Task{}, nil
	}

	tasks := make([]*types.Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task, err := s.GetTask(taskID)
		if err != nil {
			continue
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (s *RedisTaskStore) GetActiveTask(userID int64) (*types.Task, error) {
	tasks, err := s.GetUserTasks(userID)
	if err != nil {
		return nil, err
	}

	for _, task := range tasks {
		if task.State == types.StateProcessing || task.State == types.StateWaitingFile {
			return task, nil
		}
	}

	if len(tasks) > 0 {
		return tasks[len(tasks)-1], nil
	}

	return nil, fmt.Errorf("no tasks found for user")
}

func (s *RedisTaskStore) UpdateTask(task *types.Task) error {
	task.UpdatedAt = time.Now()
	task.ExpiresAt = time.Now().Add(s.ttl)

	taskKey := s.client.generateKey("task", task.ID)
	return s.client.Set(taskKey, task, s.ttl)
}

func (s *RedisTaskStore) UpdateTaskState(taskID string, state types.ChatState) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	task.State = state
	task.UpdatedAt = time.Now()

	return s.UpdateTask(task)
}

func (s *RedisTaskStore) GetProcessingTasks() ([]*types.Task, error) {
	pattern := s.client.generateKey("task", "*")
	keys, err := s.client.Keys(pattern)
	if err != nil {
		return nil, err
	}

	var processingTasks []*types.Task
	for _, key := range keys {
		var task types.Task
		if err := s.client.Get(key, &task); err != nil {
			continue
		}

		if task.State == types.StateProcessing {
			processingTasks = append(processingTasks, &task)
		}
	}

	return processingTasks, nil
}

func (s *RedisTaskStore) DeleteTask(taskID string) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	taskKey := s.client.generateKey("task", taskID)
	if err := s.client.Del(taskKey); err != nil {
		return err
	}

	userTasksKey := s.client.generateKey("user_tasks", fmt.Sprintf("%d", task.UserID))
	var taskIDs []string
	if err := s.client.Get(userTasksKey, &taskIDs); err == nil {
		newTaskIDs := make([]string, 0, len(taskIDs))
		for _, id := range taskIDs {
			if id != taskID {
				newTaskIDs = append(newTaskIDs, id)
			}
		}
		if len(newTaskIDs) > 0 {
			s.client.Set(userTasksKey, newTaskIDs, s.ttl)
		} else {
			s.client.Del(userTasksKey)
		}
	}

	return nil
}

func (s *RedisTaskStore) SetProcessingFile(userID int64, fileID, fileName string, fileSize int64) (*types.Task, error) {
	task := &types.Task{
		UserID:      userID,
		State:       types.StateChooseExt,
		FileID:      fileID,
		FileName:    fileName,
		TargetExt:   "",
		OriginalExt: getFileExtension(fileName),
		Options: map[string]interface{}{
			"file_size": fileSize,
		},
	}

	if err := s.CreateTask(task); err != nil {
		return nil, err
	}

	return task, nil
}

func (s *RedisTaskStore) SetTaskReady(taskID, resultFileID string) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	task.State = types.StateReady
	task.ResultFileID = ""
	task.ResultPath = ""
	task.UpdatedAt = time.Now()

	task.ResultFileID = resultFileID

	return s.UpdateTask(task)
}

func (s *RedisTaskStore) SetTaskError(taskID string, errorMsg string) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	task.State = types.StateError
	task.Error = errorMsg
	task.UpdatedAt = time.Now()

	return s.UpdateTask(task)
}

func (s *RedisTaskStore) CleanExpiredTasks() error {
	pattern := s.client.generateKey("task", "*")

	keys, err := s.client.Keys(pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		exists, err := s.client.Exists(key)
		if err != nil {
			continue
		}

		if !exists {
			continue
		}

		ttl, err := s.client.TTL(key)
		if err != nil {
			continue
		}

		if ttl < 0 {
			s.client.Del(key)
		}
	}

	return nil
}

func getFileExtension(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			if i == len(filename)-1 {
				return ""
			}
			return filename[i+1:]
		}
	}
	return ""
}
