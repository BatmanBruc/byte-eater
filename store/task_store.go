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

func NewTaskStore(redisClient *RedisClient, ttlHours int) *RedisTaskStore {
	ttl := time.Duration(ttlHours) * time.Hour
	if ttlHours <= 0 {
		ttl = 24 * time.Hour
	}

	return &RedisTaskStore{
		client: redisClient,
		ttl:    ttl,
	}
}

func (s *RedisTaskStore) CreateSession(session *types.Session) error {
	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	now := time.Now()
	session.CreatedAt = now
	session.UpdatedAt = now
	session.ExpiresAt = now.Add(s.ttl)

	sessionKey := s.client.generateKey("session", session.ID)
	if err := s.client.Set(sessionKey, session, s.ttl); err != nil {
		return err
	}

	userSessionKey := s.client.generateKey("user_session", fmt.Sprintf("%d", session.UserID))
	if err := s.client.Set(userSessionKey, session.ID, s.ttl); err != nil {
		s.client.Del(sessionKey)
		return err
	}

	return nil
}

func (s *RedisTaskStore) GetSession(sessionID string) (*types.Session, error) {
	sessionKey := s.client.generateKey("session", sessionID)

	var session types.Session
	if err := s.client.Get(sessionKey, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (s *RedisTaskStore) GetUserSession(userID int64) (*types.Session, error) {
	userSessionKey := s.client.generateKey("user_session", fmt.Sprintf("%d", userID))

	var sessionID string
	if err := s.client.Get(userSessionKey, &sessionID); err != nil {
		return nil, err
	}

	return s.GetSession(sessionID)
}

func (s *RedisTaskStore) UpdateSession(session *types.Session) error {
	session.UpdatedAt = time.Now()
	session.ExpiresAt = time.Now().Add(s.ttl)

	sessionKey := s.client.generateKey("session", session.ID)
	return s.client.Set(sessionKey, session, s.ttl)
}

func (s *RedisTaskStore) UpdateSessionState(sessionID string, state types.ChatState) error {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return err
	}

	session.State = state
	session.UpdatedAt = time.Now()

	return s.UpdateSession(session)
}

func (s *RedisTaskStore) DeleteSession(sessionID string) error {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return err
	}

	sessionKey := s.client.generateKey("session", sessionID)
	if err := s.client.Del(sessionKey); err != nil {
		return err
	}

	userSessionKey := s.client.generateKey("user_session", fmt.Sprintf("%d", session.UserID))
	if err := s.client.Del(userSessionKey); err != nil {
		return err
	}

	tasks, err := s.GetSessionTasks(sessionID)
	if err == nil {
		for _, task := range tasks {
			s.DeleteTask(task.ID)
		}
	}

	return nil
}

func (s *RedisTaskStore) CreateTask(task *types.Task) error {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.ExpiresAt = now.Add(s.ttl)

	if task.TargetExt == "" && task.SessionID != "" {
		session, err := s.GetSession(task.SessionID)
		if err == nil && session != nil {
			task.TargetExt = session.TargetExt
		}
	}

	taskKey := s.client.generateKey("task", task.ID)
	if err := s.client.Set(taskKey, task, s.ttl); err != nil {
		return err
	}

	sessionTasksKey := s.client.generateKey("session_tasks", task.SessionID)
	var taskIDs []string
	_ = s.client.Get(sessionTasksKey, &taskIDs)
	taskIDs = append(taskIDs, task.ID)
	if err := s.client.Set(sessionTasksKey, taskIDs, s.ttl); err != nil {
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

func (s *RedisTaskStore) GetSessionTasks(sessionID string) ([]*types.Task, error) {
	sessionTasksKey := s.client.generateKey("session_tasks", sessionID)

	var taskIDs []string
	if err := s.client.Get(sessionTasksKey, &taskIDs); err != nil {
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

func (s *RedisTaskStore) GetActiveTask(sessionID string) (*types.Task, error) {
	tasks, err := s.GetSessionTasks(sessionID)
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

	return nil, fmt.Errorf("no tasks found for session")
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

	sessionTasksKey := s.client.generateKey("session_tasks", task.SessionID)
	var taskIDs []string
	if err := s.client.Get(sessionTasksKey, &taskIDs); err == nil {
		newTaskIDs := make([]string, 0, len(taskIDs))
		for _, id := range taskIDs {
			if id != taskID {
				newTaskIDs = append(newTaskIDs, id)
			}
		}
		if len(newTaskIDs) > 0 {
			s.client.Set(sessionTasksKey, newTaskIDs, s.ttl)
		} else {
			s.client.Del(sessionTasksKey)
		}
	}

	return nil
}

func (s *RedisTaskStore) SetWaitingFile(sessionID string, targetExt string) (*types.Session, error) {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	session.State = types.StateWaitingFile
	session.TargetExt = targetExt
	session.UpdatedAt = time.Now()

	if err := s.UpdateSession(session); err != nil {
		return nil, err
	}

	return session, nil
}

func (s *RedisTaskStore) SetProcessingFile(sessionID string, fileID, fileName string) (*types.Task, error) {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %v", err)
	}

	task := &types.Task{
		SessionID: sessionID,

		State:       types.StateChooseExt,
		FileID:      fileID,
		FileName:    fileName,
		TargetExt:   "",
		OriginalExt: getFileExtension(fileName),
	}

	if err := s.CreateTask(task); err != nil {
		return nil, err
	}

	session.State = types.StateChooseExt
	s.UpdateSession(session)

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
