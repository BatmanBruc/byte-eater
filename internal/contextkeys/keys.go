package contextkeys

import "context"

type messageTypeKey struct{}
type filesInfoKey struct{}
type sessionIDKey struct{}
type callbackDataKey struct{}

type MessageType string

const (
	MessageTypeText        MessageType = "text"
	MessageTypePhoto       MessageType = "photo"
	MessageTypeVideo       MessageType = "video"
	MessageTypeDocument    MessageType = "document"
	MessageTypeAudio       MessageType = "audio"
	MessageTypeVoice       MessageType = "voice"
	MessageTypeSticker     MessageType = "sticker"
	MessageTypeLocation    MessageType = "location"
	MessageTypeContact     MessageType = "contact"
	MessageTypePoll        MessageType = "poll"
	MessageTypeUnknown     MessageType = "unknown"
	MessageTypeCommand     MessageType = "command"
	MessageTypeClickButton MessageType = "ckickButton"
)

type FileInfo struct {
	FileType MessageType `json:"file_type"`
	FileID   string      `json:"file_id"`
	FileSize int64       `json:"file_size,omitempty"`
	MimeType string      `json:"mime_type,omitempty"`
	FileName string      `json:"file_name,omitempty"`
	Duration int         `json:"duration,omitempty"`
	Width    int         `json:"width,omitempty"`
	Height   int         `json:"height,omitempty"`
}

type FilesInfo struct {
	TotalFiles int        `json:"total_files"`
	Files      []FileInfo `json:"files"`
	HasFiles   bool       `json:"has_files"`
}

func WithMessageType(ctx context.Context, msgType MessageType) context.Context {
	return context.WithValue(ctx, messageTypeKey{}, msgType)
}

func GetMessageType(ctx context.Context) (MessageType, bool) {
	v := ctx.Value(messageTypeKey{})
	if v == nil {
		return MessageTypeUnknown, false
	}
	return v.(MessageType), true
}

func WithFilesInfo(ctx context.Context, info *FilesInfo) context.Context {
	return context.WithValue(ctx, filesInfoKey{}, info)
}

func GetFilesInfo(ctx context.Context) (*FilesInfo, bool) {
	v := ctx.Value(filesInfoKey{})
	if v == nil {
		return nil, false
	}
	return v.(*FilesInfo), true
}

func IsTextMessage(ctx context.Context) bool {
	msgType, ok := GetMessageType(ctx)
	return ok && msgType == MessageTypeText
}

func HasFiles(ctx context.Context) bool {
	info, ok := GetFilesInfo(ctx)
	return ok && info != nil && info.HasFiles
}

func GetFilesCount(ctx context.Context) int {
	info, ok := GetFilesInfo(ctx)
	if !ok || info == nil {
		return 0
	}
	return info.TotalFiles
}

func GetFileInfo(ctx context.Context, index int) (FileInfo, bool) {
	info, ok := GetFilesInfo(ctx)
	if !ok || info == nil || index < 0 || index >= len(info.Files) {
		return FileInfo{}, false
	}
	return info.Files[index], true
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

func GetSessionID(ctx context.Context) (string, bool) {
	v := ctx.Value(sessionIDKey{})
	if v == nil {
		return "", false
	}
	return v.(string), true
}

func WithCallbackData(ctx context.Context, data string) context.Context {
	return context.WithValue(ctx, callbackDataKey{}, data)
}

func GetCallbackData(ctx context.Context) (string, bool) {
	v := ctx.Value(callbackDataKey{})
	if v == nil {
		return "", false
	}
	return v.(string), true
}
