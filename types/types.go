package types

import "time"

type PendingSelection struct {
	MessageID int    `json:"message_id"`
	TaskID    string `json:"task_id"`
}

type Session struct {
	ID        string                 `json:"id"`
	UserID    int64                  `json:"user_id"`
	ChatID    int64                  `json:"chat_id"`
	State     ChatState              `json:"state"`
	TargetExt string                 `json:"target_ext,omitempty"`
	Pending   []PendingSelection     `json:"pending,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	ExpiresAt time.Time              `json:"expires_at"`
}

type Task struct {
	ID           string                 `json:"id"`
	UserID       int64                  `json:"user_id"`
	State        ChatState              `json:"state"`
	FileID       string                 `json:"file_id,omitempty"`
	FileName     string                 `json:"file_name,omitempty"`
	OriginalExt  string                 `json:"original_ext,omitempty"`
	TargetExt    string                 `json:"target_ext,omitempty"`
	Options      map[string]interface{} `json:"options,omitempty"`
	ResultPath   string                 `json:"result_path,omitempty"`
	ResultFileID string                 `json:"result_file_id,omitempty"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
}

// UserStateStore хранит пользовательское состояние вне конкретных тасок:
// язык, режимы (merge/batch), временные списки и т.п.
type UserStateStore interface {
	GetUserOptions(userID int64) (map[string]interface{}, error)
	SetUserOptions(userID int64, options map[string]interface{}) error
	GetUserPending(userID int64) ([]PendingSelection, error)
	SetUserPending(userID int64, pending []PendingSelection) error
}

type TaskStore interface {
	CreateTask(task *Task) error
	GetTask(taskID string) (*Task, error)
	GetUserTasks(userID int64) ([]*Task, error)
	GetActiveTask(userID int64) (*Task, error)
	UpdateTask(task *Task) error
	UpdateTaskState(taskID string, state ChatState) error
	DeleteTask(taskID string) error

	SetProcessingFile(userID int64, fileID, fileName string, fileSize int64) (*Task, error)
	GetProcessingTasks() ([]*Task, error)

	SetTaskReady(taskID, resultFileID string) error
	SetTaskError(taskID string, errorMsg string) error
	CleanExpiredTasks() error
}
