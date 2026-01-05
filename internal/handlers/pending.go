package handlers

import (
	"context"
	"strings"

	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/utils"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) refreshPendingSelections(ctx context.Context, b *bot.Bot, userID int64, lang i18n.Lang, unlimited bool, remaining int, excludeMessageID int, excludeTaskID string) {
	pending, _ := bh.userState.GetUserPending(userID)
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	next := make([]types.PendingSelection, 0, len(pending))
	for _, p := range pending {
		if p.MessageID == 0 || strings.TrimSpace(p.TaskID) == "" {
			continue
		}
		if excludeMessageID != 0 && p.MessageID == excludeMessageID {
			next = append(next, p)
			continue
		}
		if excludeTaskID != "" && p.TaskID == excludeTaskID {
			next = append(next, p)
			continue
		}
		task, err := bh.store.GetTask(p.TaskID)
		if err != nil || task == nil {
			continue
		}
		if task.State != types.StateChooseExt {
			continue
		}
		textInput := false
		if task.Options != nil {
			if v, ok := task.Options["text_input"]; ok {
				if bv, ok := v.(bool); ok {
					textInput = bv
				}
			}
		}

		buttons := []formats.FormatButton{}
		if textInput {
			buttons = formats.GetTextOutputButtons(task.ID)
		} else {
			buttons = formats.GetButtonsForSourceExt(task.OriginalExt, task.ID, lang)
		}
		text := ""
		if textInput {
			text = messages.TextReceivedChooseFormat(lang)
		} else {
			text = messages.FileReceivedChooseFormat(lang, task.FileName)
		}
		chatID := task.UserID
		_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: p.MessageID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: utils.BuildInlineKeyboard(buttons).InlineKeyboard,
			},
		})
		if err != nil {
			continue
		}
		next = append(next, p)
	}
	_ = bh.userState.SetUserPending(userID, next)
}

func (bh *Handlers) removePendingSelection(userID int64, messageID int, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if messageID == 0 && taskID == "" {
		return
	}
	pending, _ := bh.userState.GetUserPending(userID)
	next := make([]types.PendingSelection, 0, len(pending))
	for _, p := range pending {
		if p.MessageID == 0 || strings.TrimSpace(p.TaskID) == "" {
			continue
		}
		if messageID != 0 && p.MessageID == messageID {
			continue
		}
		if taskID != "" && p.TaskID == taskID {
			continue
		}
		next = append(next, p)
	}
	_ = bh.userState.SetUserPending(userID, next)
}

func (bh *Handlers) addPendingSelection(userID int64, messageID int, taskID string) {
	if messageID == 0 || strings.TrimSpace(taskID) == "" {
		return
	}
	pending, _ := bh.userState.GetUserPending(userID)
	next := make([]types.PendingSelection, 0, len(pending)+1)
	for _, p := range pending {
		if p.MessageID == 0 || strings.TrimSpace(p.TaskID) == "" {
			continue
		}
		if p.MessageID == messageID || p.TaskID == taskID {
			continue
		}
		next = append(next, p)
	}
	next = append(next, types.PendingSelection{MessageID: messageID, TaskID: taskID})
	if len(next) > 25 {
		next = next[len(next)-25:]
	}
	_ = bh.userState.SetUserPending(userID, next)
}
