package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/utils"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) HandleFile(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	filesInfo, hasFiles := contextkeys.GetFilesInfo(ctx)
	lang := bh.langFromUserOrCtx(ctx, userID)
	if !hasFiles || filesInfo == nil || len(filesInfo.Files) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.ErrorCannotProcessFile(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	options, _ := bh.userState.GetUserOptions(userID)
	if options == nil {
		options = map[string]interface{}{}
	}

	if st, ok := options["merge_state"].(string); ok && strings.TrimSpace(st) == "waiting" {
		bh.handleMergePDFFile(ctx, b, userID, lang, filesInfo.Files)
		return
	}

	if st, ok := options["mb_state"].(string); ok && strings.TrimSpace(st) == "collect" {
		bh.manualBatchAddFiles(ctx, b, userID, lang, filesInfo.Files)
		return
	}

	for _, fi := range filesInfo.Files {
		f := formats.BatchFile{FileID: fi.FileID, FileName: fi.FileName, FileSize: fi.FileSize}
		bh.createAndAskFormatForSingleFile(ctx, b, userID, lang, f)
	}
}

func (bh *Handlers) handleBatchChoice(ctx context.Context, b *bot.Bot, update *models.Update, userID int64, lang i18n.Lang, batchTaskID string, choice string) {
	task, err := bh.store.GetTask(batchTaskID)
	if err != nil || task == nil || task.Options == nil {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotFound(lang))
		return
	}
	if task.UserID != userID {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotInSession(lang))
		return
	}

	files := parseBatchFiles(task.Options["batch_files"])
	if len(files) < 2 {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}

	chatID := getChatIDFromUpdate(update)
	if chatID == 0 {
		chatID = userID
	}

	if choice == "batch_sep" {
		if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
			msg := update.CallbackQuery.Message.Message
			_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
		}
		_ = bh.store.DeleteTask(task.ID)
		for _, f := range files {
			bh.createAndAskFormatForSingleFile(ctx, b, userID, lang, f)
		}
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		return
	}

	if choice == "batch_all" {
		task.Options["batch_mode"] = "all"
		_ = bh.store.UpdateTask(task)
		buttons := formats.GetBatchButtonsBySourceExt(task.OriginalExt, task.ID, files, lang)
		if len(buttons) == 0 {
			_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.ErrorNoConversionOptions(lang, ""))
			return
		}
		keyboard := utils.BuildInlineKeyboard(buttons)
		text := messages.BatchChooseFormat(lang, task.OriginalExt, len(files))

		if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
			msg := update.CallbackQuery.Message.Message
			_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
		}
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: keyboard.InlineKeyboard,
			},
		})
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		return
	}

	_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
}

func (bh *Handlers) createAndAskFormatForSingleFile(ctx context.Context, b *bot.Bot, userID int64, lang i18n.Lang, f formats.BatchFile) {
	fileName := strings.TrimSpace(f.FileName)
	if fileName == "" {
		fileName = fmt.Sprintf("file_%d.txt", time.Now().UnixNano())
	}
	ext := bh.getExtensionFromFileName(fileName)
	task, err := bh.store.SetProcessingFile(userID, f.FileID, fileName, f.FileSize)
	if err != nil {
		return
	}
	if task.Options == nil {
		task.Options = map[string]interface{}{}
	}
	task.Options["lang"] = string(lang)
	_ = bh.store.UpdateTask(task)
	buttons := formats.GetButtonsForSourceExt(ext, task.ID, lang)
	if len(buttons) == 0 {
		return
	}
	keyboard := utils.BuildInlineKeyboard(buttons)
	text := messages.FileReceivedChooseFormat(lang, fileName)
	chatID := userID
	if bh.billing != nil {
		unlimited, err := bh.billing.IsUnlimited(userID)
		if err == nil && unlimited {
			text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
		}
	}
	sent, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
	if sent != nil {
		bh.addPendingSelection(userID, sent.ID, task.ID)
	}
}

func (bh *Handlers) handleBatchFormatSelection(ctx context.Context, b *bot.Bot, update *models.Update, userID int64, lang i18n.Lang, batchTask *types.Task, targetExt string) {
	files := parseBatchFiles(batchTask.Options["batch_files"])
	if len(files) < 2 {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}
	unlimited := false
	if bh.billing != nil {
		u, _ := bh.billing.IsUnlimited(userID)
		unlimited = u
	}

	chatID := getChatIDFromUpdate(update)
	if chatID == 0 {
		chatID = userID
	}

	for _, f := range files {
		task := &types.Task{
			UserID:      userID,
			State:       types.StateProcessing,
			FileID:      f.FileID,
			FileName:    f.FileName,
			OriginalExt: batchTask.OriginalExt,
			TargetExt:   targetExt,
			Options: map[string]interface{}{
				"file_size":         f.FileSize,
				"lang":              string(lang),
				"unlimited":         unlimited,
				"priority":          unlimited,
				"batch_parent_task": batchTask.ID,
			},
		}
		_ = bh.store.CreateTask(task)
		bh.scheduler.EnqueueTask(task.ID, chatID, 0, task.FileName, lang, unlimited)
	}

	_ = bh.store.DeleteTask(batchTask.ID)

	if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
		msg := update.CallbackQuery.Message.Message
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
	}
	text := messages.BatchStarted(lang, len(files))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: messages.ParseModeHTML,
	})
	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
}

func (bh *Handlers) getExtensionFromFileName(fileName string) string {
	parts := strings.Split(fileName, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}
