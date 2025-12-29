package types

import "time"

type Session struct {
	ID        string                 `json:"id"`
	UserID    int64                  `json:"user_id"`
	ChatID    int64                  `json:"chat_id"`
	State     ChatState              `json:"state"`
	TargetExt string                 `json:"target_ext,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	ExpiresAt time.Time              `json:"expires_at"`
}

type Task struct {
	ID           string                 `json:"id"`
	SessionID    string                 `json:"session_id"`
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

type TaskStore interface {
	CreateSession(session *Session) error
	GetSession(sessionID string) (*Session, error)
	GetUserSession(userID int64) (*Session, error)
	UpdateSession(session *Session) error
	UpdateSessionState(sessionID string, state ChatState) error
	DeleteSession(sessionID string) error

	CreateTask(task *Task) error
	GetTask(taskID string) (*Task, error)
	GetSessionTasks(sessionID string) ([]*Task, error)
	GetActiveTask(sessionID string) (*Task, error)
	UpdateTask(task *Task) error
	UpdateTaskState(taskID string, state ChatState) error
	DeleteTask(taskID string) error

	SetWaitingFile(sessionID string, targetExt string) (*Session, error)
	SetProcessingFile(sessionID string, fileID, fileName string, fileSize int64) (*Task, error)
	GetProcessingTasks() ([]*Task, error)

	SetTaskReady(taskID, resultFileID string) error
	SetTaskError(taskID string, errorMsg string) error
	CleanExpiredTasks() error
}
