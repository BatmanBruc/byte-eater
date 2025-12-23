package middleware

import (
	"context"
	"log"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
)

type Middlewares struct {
	store types.TaskStore
}

func NewMessageAnalyzer(store types.TaskStore) *Middlewares {
	return &Middlewares{
		store: store,
	}
}

func (m *Middlewares) CheckTaskMiddleWare(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var (
			userID int64
			chatID int64
		)

		switch {
		case update.Message != nil && update.Message.From != nil:
			userID = update.Message.From.ID
			chatID = update.Message.Chat.ID
		case update.CallbackQuery != nil:
			userID = update.CallbackQuery.From.ID
			chatID = getChatIDFromMaybeInaccessibleMessage(update.CallbackQuery.Message)
			if chatID == 0 {
				return
			}
		default:
			return
		}

		if userID == 0 || chatID == 0 {
			return
		}

		session, err := m.store.GetUserSession(userID)

		if err != nil {
			session = &types.Session{
				UserID: userID,
				ChatID: chatID,
				State:  types.StateStart,
			}
			err = m.store.CreateSession(session)
			if err != nil {
				log.Printf("Error creating session: %v", err)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:    chatID,
					Text:      messages.ErrorDefault(),
					ParseMode: messages.ParseModeHTML,
				})
				return
			}
		}

		ctx = contextkeys.WithSessionID(ctx, session.ID)
		next(ctx, b, update)
	}
}

func getChatIDFromMaybeInaccessibleMessage(m models.MaybeInaccessibleMessage) int64 {

	if m.Message != nil {
		return m.Message.Chat.ID
	}
	if m.InaccessibleMessage != nil {
		return m.InaccessibleMessage.Chat.ID
	}
	return 0
}

func (ma *Middlewares) AnalyzeMessageMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var newCtx context.Context

		if update.CallbackQuery != nil && update.CallbackQuery.Data != "" {
			newCtx = contextkeys.WithMessageType(ctx, contextkeys.MessageTypeClickButton)
			newCtx = contextkeys.WithCallbackData(newCtx, update.CallbackQuery.Data)
			next(newCtx, b, update)
			return
		}

		if update.Message != nil && update.Message.Text != "" && strings.HasPrefix(update.Message.Text, "/") {
			newCtx = contextkeys.WithMessageType(ctx, contextkeys.MessageTypeCommand)
		} else {
			newCtx = ma.analyzeMessage(ctx, update)
		}

		next(newCtx, b, update)
	}
}

func (ma *Middlewares) analyzeMessage(ctx context.Context, update *models.Update) context.Context {
	if update.Message == nil {
		return ctx
	}

	msg := update.Message
	var msgType contextkeys.MessageType
	var filesInfo *contextkeys.FilesInfo

	msgType = ma.determineMessageType(msg)

	if ma.hasFiles(msg) {
		filesInfo = ma.analyzeFilesInMessage(msg)
	}

	ctx = contextkeys.WithMessageType(ctx, msgType)
	if filesInfo != nil {
		ctx = contextkeys.WithFilesInfo(ctx, filesInfo)
	}

	return ctx
}

func (ma *Middlewares) determineMessageType(msg *models.Message) contextkeys.MessageType {

	if len(msg.Photo) > 0 {
		return contextkeys.MessageTypePhoto
	}

	if msg.Video != nil {
		return contextkeys.MessageTypeVideo
	}

	if msg.Document != nil {
		return contextkeys.MessageTypeDocument
	}

	if msg.Audio != nil {
		return contextkeys.MessageTypeAudio
	}

	if msg.Voice != nil {
		return contextkeys.MessageTypeVoice
	}

	if msg.Sticker != nil {
		return contextkeys.MessageTypeSticker
	}

	if msg.VideoNote != nil {
		return contextkeys.MessageTypeVideo
	}

	if msg.Location != nil {
		return contextkeys.MessageTypeLocation
	}

	if msg.Contact != nil {
		return contextkeys.MessageTypeContact
	}

	if msg.Poll != nil {
		return contextkeys.MessageTypePoll
	}

	if msg.Text != "" || msg.Caption != "" {
		return contextkeys.MessageTypeText
	}

	return contextkeys.MessageTypeUnknown
}

func (ma *Middlewares) hasFiles(msg *models.Message) bool {
	return len(msg.Photo) > 0 ||
		msg.Video != nil ||
		msg.Document != nil ||
		msg.Audio != nil ||
		msg.Voice != nil ||
		msg.Sticker != nil ||
		msg.VideoNote != nil
}

func (ma *Middlewares) analyzeFilesInMessage(msg *models.Message) *contextkeys.FilesInfo {
	files := make([]contextkeys.FileInfo, 0)

	if len(msg.Photo) > 0 {
		best := msg.Photo[0]
		for i := 1; i < len(msg.Photo); i++ {
			if msg.Photo[i].FileSize > best.FileSize {
				best = msg.Photo[i]
			}
		}
		files = append(files, contextkeys.FileInfo{
			FileType: contextkeys.MessageTypePhoto,
			FileID:   best.FileID,
			FileSize: int64(best.FileSize),
			Width:    best.Width,
			Height:   best.Height,
			FileName: "photo.jpg",
		})
	}

	if msg.Video != nil {
		files = append(files, ma.analyzeVideo(msg.Video))
	}

	if msg.Document != nil {
		files = append(files, ma.analyzeDocument(msg.Document))
	}

	if msg.Audio != nil {
		files = append(files, ma.analyzeAudio(msg.Audio))
	}

	if msg.Voice != nil {
		files = append(files, ma.analyzeVoice(msg.Voice))
	}

	if msg.Sticker != nil {
		files = append(files, ma.analyzeSticker(msg.Sticker))
	}

	if msg.VideoNote != nil {
		files = append(files, ma.analyzeVideoNote(msg.VideoNote))
	}

	return &contextkeys.FilesInfo{
		TotalFiles: len(files),
		Files:      files,
		HasFiles:   len(files) > 0,
	}
}

func (ma *Middlewares) analyzeVideo(video *models.Video) contextkeys.FileInfo {
	fileName := video.FileName
	if fileName == "" {
		fileName = "video." + ma.getExtensionFromMimeType(video.MimeType, "mp4")
	} else if !strings.Contains(fileName, ".") {
		ext := ma.getExtensionFromMimeType(video.MimeType, "mp4")
		fileName = fileName + "." + ext
	}

	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeVideo,
		FileID:   video.FileID,
		FileSize: int64(video.FileSize),
		MimeType: video.MimeType,
		FileName: fileName,
		Duration: video.Duration,
		Width:    video.Width,
		Height:   video.Height,
	}
}

func (ma *Middlewares) analyzeDocument(doc *models.Document) contextkeys.FileInfo {
	fileName := doc.FileName
	if fileName == "" {
		fileName = "document." + ma.getExtensionFromMimeType(doc.MimeType, "")
	} else if !strings.Contains(fileName, ".") {
		ext := ma.getExtensionFromMimeType(doc.MimeType, "")
		if ext != "" {
			fileName = fileName + "." + ext
		}
	}

	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeDocument,
		FileID:   doc.FileID,
		FileSize: int64(doc.FileSize),
		MimeType: doc.MimeType,
		FileName: fileName,
	}
}

func (ma *Middlewares) analyzeAudio(audio *models.Audio) contextkeys.FileInfo {
	fileName := audio.FileName
	if fileName == "" {
		fileName = "audio." + ma.getExtensionFromMimeType(audio.MimeType, "mp3")
	} else if !strings.Contains(fileName, ".") {
		ext := ma.getExtensionFromMimeType(audio.MimeType, "mp3")
		fileName = fileName + "." + ext
	}

	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeAudio,
		FileID:   audio.FileID,
		FileSize: int64(audio.FileSize),
		MimeType: audio.MimeType,
		FileName: fileName,
		Duration: audio.Duration,
	}
}

func (ma *Middlewares) analyzeVoice(voice *models.Voice) contextkeys.FileInfo {
	fileName := "voice." + ma.getExtensionFromMimeType(voice.MimeType, "ogg")

	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeVoice,
		FileID:   voice.FileID,
		FileSize: int64(voice.FileSize),
		MimeType: voice.MimeType,
		FileName: fileName,
		Duration: voice.Duration,
	}
}

func (ma *Middlewares) analyzeSticker(sticker *models.Sticker) contextkeys.FileInfo {
	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeSticker,
		FileID:   sticker.FileID,
		FileSize: int64(sticker.FileSize),
		Width:    sticker.Width,
		Height:   sticker.Height,
	}
}

func (ma *Middlewares) analyzeVideoNote(videoNote *models.VideoNote) contextkeys.FileInfo {
	return contextkeys.FileInfo{
		FileType: contextkeys.MessageTypeVideo,
		FileID:   videoNote.FileID,
		FileSize: int64(videoNote.FileSize),
		FileName: "video_note.mp4",
		Duration: videoNote.Duration,
	}
}

func (ma *Middlewares) getExtensionFromMimeType(mimeType string, defaultExt string) string {
	if mimeType == "" {
		if defaultExt != "" {
			return defaultExt
		}
		return ""
	}

	parts := strings.Split(mimeType, "/")
	if len(parts) != 2 {
		if defaultExt != "" {
			return defaultExt
		}
		return ""
	}

	subtype := parts[1]
	if strings.Contains(subtype, ";") {
		subtype = strings.Split(subtype, ";")[0]
	}

	mimeToExt := map[string]string{
		"jpeg":    "jpg",
		"jpg":     "jpg",
		"png":     "png",
		"gif":     "gif",
		"webp":    "webp",
		"bmp":     "bmp",
		"tiff":    "tiff",
		"tif":     "tif",
		"ico":     "ico",
		"heic":    "heic",
		"avif":    "avif",
		"psd":     "psd",
		"svg":     "svg",
		"apng":    "apng",
		"eps":     "eps",
		"jp2":     "jp2",
		"tgs":     "tgs",
		"pdf":     "pdf",
		"zip":     "zip",
		"mp4":     "mp4",
		"mp3":     "mp3",
		"ogg":     "ogg",
		"wav":     "wav",
		"flac":    "flac",
		"wma":     "wma",
		"oga":     "oga",
		"m4a":     "m4a",
		"aac":     "aac",
		"aiff":    "aiff",
		"amr":     "amr",
		"opus":    "opus",
		"avi":     "avi",
		"wmv":     "wmv",
		"mkv":     "mkv",
		"3gp":     "3gp",
		"3gpp":    "3gpp",
		"mpg":     "mpg",
		"mpeg":    "mpeg",
		"webm":    "webm",
		"ts":      "ts",
		"mov":     "mov",
		"flv":     "flv",
		"asf":     "asf",
		"vob":     "vob",
		"doc":     "doc",
		"docx":    "docx",
		"xls":     "xls",
		"xlsx":    "xlsx",
		"ppt":     "ppt",
		"pptx":    "pptx",
		"pptm":    "pptm",
		"pps":     "pps",
		"ppsx":    "ppsx",
		"ppsm":    "ppsm",
		"pot":     "pot",
		"potx":    "potx",
		"potm":    "potm",
		"odp":     "odp",
		"odt":     "odt",
		"ods":     "ods",
		"txt":     "txt",
		"rtf":     "rtf",
		"torrent": "torrent",
		"epub":    "epub",
		"mobi":    "mobi",
		"azw3":    "azw3",
		"lrf":     "lrf",
		"pdb":     "pdb",
		"cbr":     "cbr",
		"fb2":     "fb2",
		"cbz":     "cbz",
		"djvu":    "djvu",
		"ttf":     "ttf",
		"otf":     "otf",
		"eot":     "eot",
		"woff":    "woff",
		"woff2":   "woff2",
		"pfb":     "pfb",
		"srt":     "srt",
		"vtt":     "vtt",
		"stl":     "stl",
		"sbv":     "sbv",
		"sub":     "sub",
		"ass":     "ass",
		"ssa":     "ssa",
		"lrc":     "lrc",
		"dfxp":    "dfxp",
		"ttml":    "ttml",
		"qt.txt":  "qt.txt",
	}

	ext := mimeToExt[strings.ToLower(subtype)]
	if ext != "" {
		return ext
	}

	if defaultExt != "" {
		return defaultExt
	}

	return strings.ToLower(subtype)
}
