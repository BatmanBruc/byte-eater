package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type TaskEnqueuer interface {
	EnqueueTask(taskID string, chatID int64, messageID int, fileName string) int
}

type Handlers struct {
	store     types.TaskStore
	scheduler TaskEnqueuer
}

func NewHandlers(store types.TaskStore, scheduler TaskEnqueuer) *Handlers {
	return &Handlers{
		store:     store,
		scheduler: scheduler,
	}
}

func (bh *Handlers) MainHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := bh.getChatIDFromUpdate(update)
	messageType, _ := contextkeys.GetMessageType(ctx)

	sessionID, ok := contextkeys.GetSessionID(ctx)
	if !ok {
		log.Printf("Error: SessionID not found in context")
		if chatID != 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorDefault(),
				ParseMode: messages.ParseModeHTML,
			})
		}
		return
	}

	session, err := bh.store.GetSession(sessionID)
	if err != nil {
		log.Printf("Error getting session: %v", err)
		if chatID != 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorDefault(),
				ParseMode: messages.ParseModeHTML,
			})
		}
		return
	}

	switch messageType {
	case contextkeys.MessageTypeCommand:
		bh.HandleCommand(ctx, b, update, session)
	case contextkeys.MessageTypeDocument, contextkeys.MessageTypePhoto, contextkeys.MessageTypeVideo,
		contextkeys.MessageTypeAudio, contextkeys.MessageTypeVoice:
		bh.HandleFile(ctx, b, update, session)
	case contextkeys.MessageTypeText:
		bh.HandleText(ctx, b, update, session)
	case contextkeys.MessageTypeClickButton:
		bh.HandleClickButton(ctx, b, update, session)
	default:
		if chatID != 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorUnsupportedMessageType(),
				ParseMode: messages.ParseModeHTML,
			})
		}
	}
}

func (bh *Handlers) getChatIDFromUpdate(update *models.Update) int64 {
	if update == nil {
		return 0
	}
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil {
		if update.CallbackQuery.Message.Message != nil {
			return update.CallbackQuery.Message.Message.Chat.ID
		}
		if update.CallbackQuery.Message.InaccessibleMessage != nil {
			return update.CallbackQuery.Message.InaccessibleMessage.Chat.ID
		}
	}
	return 0
}

func (bh *Handlers) HandleClickButton(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	if update.CallbackQuery == nil {
		return
	}

	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")

	// Мгновенно убираем кнопки, чтобы избежать повторных нажатий.
	if update.CallbackQuery.Message.Message != nil {
		msg := update.CallbackQuery.Message.Message
		_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: [][]models.InlineKeyboardButton{},
			},
		})
	}

	data, _ := contextkeys.GetCallbackData(ctx)
	if data == "" {
		data = update.CallbackQuery.Data
	}

	format, taskID, err := bh.parseClickButtonData(data)
	if err != nil {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "Некорректные данные кнопки")
		return
	}

	format = strings.ToLower(format)
	if !formats.FormatExists(format) {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "Неподдерживаемый формат")
		return
	}

	task, err := bh.store.GetTask(taskID)
	if err != nil {
		log.Printf("Error getting task %s: %v", taskID, err)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "Задача не найдена")
		return
	}

	if task.SessionID != session.ID {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "Эта задача не принадлежит текущей сессии")
		return
	}

	task.TargetExt = format
	task.State = types.StateProcessing
	if err := bh.store.UpdateTask(task); err != nil {
		log.Printf("Error updating task %s: %v", taskID, err)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "Не удалось обновить задачу")
		return
	}

	chatID := int64(0)
	messageID := 0
	if update.CallbackQuery.Message.Message != nil {
		chatID = update.CallbackQuery.Message.Message.Chat.ID
		messageID = update.CallbackQuery.Message.Message.ID
	}

	position := bh.scheduler.EnqueueTask(taskID, chatID, messageID, task.FileName)
	statusText := ""
	if position < 0 {
		statusText = messages.QueueAlreadyQueued(task.FileName)
	} else if position > 0 {
		statusText = messages.QueueQueued(task.FileName, position)
	} else {
		statusText = messages.QueueStarted(task.FileName)
	}

	// UX: сразу обновляем текст сообщения (кнопки уже убраны выше).
	if update.CallbackQuery.Message.Message != nil {
		msg := update.CallbackQuery.Message.Message

		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			Text:      statusText,
			ParseMode: messages.ParseModeHTML,
		})
	}
}

func (bh *Handlers) parseClickButtonData(data string) (format string, taskID string, err error) {

	parts := strings.Split(data, "_for_")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid callback data: %q", data)
	}
	format = strings.TrimSpace(parts[0])
	taskID = strings.TrimSpace(parts[1])
	if format == "" || taskID == "" {
		return "", "", fmt.Errorf("invalid callback data: %q", data)
	}
	return format, taskID, nil
}

func (bh *Handlers) answerCallback(ctx context.Context, b *bot.Bot, callbackID, text string) error {
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
	})
	return err
}

func (bh *Handlers) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	command := update.Message.Text

	switch command {
	case "/start":
		session.State = types.StateStart
		session.TargetExt = ""
		if err := bh.store.UpdateSession(session); err != nil {
			log.Printf("Error updating session: %v", err)
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.StartWelcome(),
			ParseMode: messages.ParseModeHTML,
		})
	case "/help":
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      formats.GetHelpMessage(),
			ParseMode: messages.ParseModeHTML,
		})
	default:
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.ErrorUnknownCommand(),
			ParseMode: messages.ParseModeHTML,
		})
	}
}

func (bh *Handlers) HandleFile(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	filesInfo, hasFiles := contextkeys.GetFilesInfo(ctx)
	if !hasFiles || filesInfo == nil || len(filesInfo.Files) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.ErrorCannotProcessFile(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	for _, fileInfo := range filesInfo.Files {
		fileName := strings.TrimSpace(fileInfo.FileName)
		// Если пришло дефолтное имя photo.jpg — делаем его уникальным, чтобы избежать коллизий.
		if strings.EqualFold(fileName, "photo.jpg") {
			fileName = fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
		}
		ext := bh.getExtensionFromFileName(fileName)
		category := formats.GetCategoryByExtension(ext)
		if category == "" {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorCannotDetectFileType(fileName),
				ParseMode: messages.ParseModeHTML,
			})
			continue
		}

		task, err := bh.store.SetProcessingFile(session.ID, fileInfo.FileID, fileName)
		if err != nil {
			log.Printf("Error setting processing file: %v", err)
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorDefault(),
				ParseMode: messages.ParseModeHTML,
			})
			continue
		}

		buttons := formats.GetFormatButtonsByCategory(category, task.ID)
		if len(buttons) == 0 {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorCannotGetFormats(),
				ParseMode: messages.ParseModeHTML,
			})
			continue
		}

		keyboard := bh.buildInlineKeyboard(buttons)

		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      update.Message.Chat.ID,
			Text:        messages.FileReceivedChooseFormat(fileName),
			ParseMode:   messages.ParseModeHTML,
			ReplyMarkup: keyboard,
		})
	}
}

func (bh *Handlers) getExtensionFromFileName(fileName string) string {
	parts := strings.Split(fileName, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

func (bh *Handlers) buildInlineKeyboard(buttons []formats.FormatButton) models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0)
	row := make([]models.InlineKeyboardButton, 0)

	for i, button := range buttons {
		if i > 0 && i%3 == 0 {
			rows = append(rows, row)
			row = make([]models.InlineKeyboardButton, 0)
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         button.Text,
			CallbackData: button.CallbackData,
		})
	}

	if len(row) > 0 {
		rows = append(rows, row)
	}

	return models.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

func (bh *Handlers) HandleText(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	if update == nil || update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.EmptyTextHint(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	tmpDir := filepath.Join(os.TempDir(), "bot_converter_text")
	_ = os.MkdirAll(tmpDir, 0755)
	tmpName := fmt.Sprintf("text_%d.txt", time.Now().Unix())
	tmpPath := filepath.Join(tmpDir, tmpName)

	if err := os.WriteFile(tmpPath, []byte(text), 0644); err != nil {
		log.Printf("Error writing temp text file: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorDefault(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}
	defer func() { _ = os.Remove(tmpPath) }()

	f, err := os.Open(tmpPath)
	if err != nil {
		log.Printf("Error opening temp text file: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorDefault(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}
	defer f.Close()

	msg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: tmpName,
			Data:     f,
		},
	})
	if err != nil || msg == nil || msg.Document == nil {
		log.Printf("Error uploading text as document: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorUploadTextAsFile(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	fileID := msg.Document.FileID
	fileName := msg.Document.FileName
	if fileName == "" {
		fileName = tmpName
	}

	_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: msg.ID,
	})

	// 3) Создаём задачу конвертации и просим выбрать целевой формат.
	task, err := bh.store.SetProcessingFile(session.ID, fileID, fileName)
	if err != nil {
		log.Printf("Error creating task for text file: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorDefault(),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	buttons := formats.GetTextOutputButtons(task.ID)
	keyboard := bh.buildInlineKeyboard(buttons)
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        messages.TextReceivedChooseFormat(),
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
}
