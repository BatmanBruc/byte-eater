package handlers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) HandleClickButton(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	if update.CallbackQuery == nil {
		return
	}
	lang := bh.langFromUserOrCtx(ctx, userID)
	chatID := int64(0)
	messageID := 0
	if update.CallbackQuery.Message.Message != nil {
		chatID = update.CallbackQuery.Message.Message.Chat.ID
		messageID = update.CallbackQuery.Message.Message.ID
	}
	if chatID == 0 {
		chatID = userID
	}

	data, _ := contextkeys.GetCallbackData(ctx)
	if data == "" {
		data = update.CallbackQuery.Data
	}

	if data == "merge_pdf" {
		bh.handleMergePDF(ctx, b, update, userID, lang)
		return
	}

	format, taskID, err := bh.parseClickButtonData(data)
	if err != nil {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}

	format = strings.ToLower(format)

	if format == "batch_sep" || format == "batch_all" {
		bh.handleBatchChoice(ctx, b, update, userID, lang, taskID, format)
		return
	}
	action := ""
	targetExt := format
	quality := 0
	maxSize := 0
	imgW := 0
	imgH := 0
	videoHeight := 0
	videoCRF := 0
	videoGIFHeight := 0
	vidW := 0
	vidH := 0
	p := strings.Split(format, "_")
	if len(p) == 3 && p[0] == "imgc" {
		action = "compress"
		targetExt = strings.ToLower(strings.TrimSpace(p[1]))
		quality, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	if len(p) == 3 && p[0] == "imgr" {
		action = "resize"
		targetExt = strings.ToLower(strings.TrimSpace(p[1]))
		maxSize, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	if len(p) == 3 && p[0] == "vidr" {
		action = "vid_resize"
		targetExt = strings.ToLower(strings.TrimSpace(p[1]))
		videoHeight, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	if len(p) == 3 && p[0] == "vidc" {
		action = "vid_compress"
		targetExt = strings.ToLower(strings.TrimSpace(p[1]))
		videoCRF, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	if len(p) == 3 && p[0] == "vidg" {
		action = "vid_gif"
		targetExt = strings.ToLower(strings.TrimSpace(p[1]))
		videoGIFHeight, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	profile := ""
	if len(p) >= 2 && p[0] == "pimg" {
		profile = strings.Join(p[1:], "_")
		action = "profile_img"
		targetExt = "jpg"
		quality = 85
		switch profile {
		case "avito":
			maxSize = 1600
		case "instagram_feed":
			imgW = 1080
			imgH = 1080
		case "instagram_story":
			imgW = 1080
			imgH = 1920
		case "vk_square":
			imgW = 1080
			imgH = 1080
		default:
			action = ""
		}
	}
	if len(p) >= 2 && p[0] == "pvid" {
		profile = strings.Join(p[1:], "_")
		action = "profile_vid"
		targetExt = "mp4"
		videoCRF = 28
		switch profile {
		case "tiktok", "reels", "shorts", "vk_clips":
			vidW = 1080
			vidH = 1920
		case "youtube_1080p":
			vidW = 1920
			vidH = 1080
		default:
			action = ""
		}
	}
	if (len(p) >= 1 && (p[0] == "pimg" || p[0] == "pvid")) && action == "" {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}

	if !formats.FormatExists(targetExt) {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackUnsupportedFormat(lang))
		return
	}

	task, err := bh.store.GetTask(taskID)
	if err != nil {
		log.Printf("Error getting task %s: %v", taskID, err)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotFound(lang))
		return
	}

	if task.UserID != userID {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotInSession(lang))
		return
	}

	if task.Options == nil {
		task.Options = map[string]interface{}{}
	}

	if v, ok := task.Options["batch_mode"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "all" {
			bh.handleBatchFormatSelection(ctx, b, update, userID, lang, task, targetExt)
			return
		}
	}
	unlimited := false
	remaining := 0
	if bh.billing != nil {
		u, _ := bh.billing.IsUnlimited(userID)
		unlimited = u
	}

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

	task.TargetExt = targetExt
	task.State = types.StateProcessing
	task.Options["unlimited"] = unlimited
	task.Options["lang"] = string(lang)
	delete(task.Options, "img_op")
	delete(task.Options, "img_quality")
	delete(task.Options, "img_max")
	delete(task.Options, "img_w")
	delete(task.Options, "img_h")
	delete(task.Options, "vid_op")
	delete(task.Options, "vid_height")
	delete(task.Options, "vid_crf")
	delete(task.Options, "vid_gif_height")
	delete(task.Options, "vid_w")
	delete(task.Options, "vid_h")
	if action != "" {
		if action == "compress" || action == "resize" {
			task.Options["img_op"] = action
		}
		if action == "compress" && quality > 0 {
			task.Options["img_quality"] = quality
		}
		if action == "resize" && maxSize > 0 {
			task.Options["img_max"] = maxSize
		}
		if action == "profile_img" {
			task.Options["img_op"] = "profile"
			if quality > 0 {
				task.Options["img_quality"] = quality
			}
			if maxSize > 0 {
				task.Options["img_max"] = maxSize
			}
			if imgW > 0 && imgH > 0 {
				task.Options["img_w"] = imgW
				task.Options["img_h"] = imgH
			}
		}
		if action == "vid_resize" {
			task.Options["vid_op"] = "resize"
			if videoHeight > 0 {
				task.Options["vid_height"] = videoHeight
			}
		}
		if action == "vid_compress" {
			task.Options["vid_op"] = "compress"
			if videoCRF > 0 {
				task.Options["vid_crf"] = videoCRF
			}
		}
		if action == "vid_gif" {
			task.Options["vid_op"] = "gif"
			if videoGIFHeight > 0 {
				task.Options["vid_gif_height"] = videoGIFHeight
			}
		}
		if action == "profile_vid" {
			task.Options["vid_op"] = "profile"
			if videoCRF > 0 {
				task.Options["vid_crf"] = videoCRF
			}
			if vidW > 0 && vidH > 0 {
				task.Options["vid_w"] = vidW
				task.Options["vid_h"] = vidH
			}
		}
	}
	if err := bh.store.UpdateTask(task); err != nil {
		log.Printf("Error updating task %s: %v", taskID, err)
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskUpdateFailed(lang))
		return
	}

	priority := unlimited
	task.Options["priority"] = priority
	position := bh.scheduler.EnqueueTask(taskID, chatID, messageID, task.FileName, lang, priority)
	statusText := ""
	if position < 0 {
		statusText = messages.QueueAlreadyQueued(lang, task.FileName)
	} else if position > 0 {
		statusText = messages.QueueQueued(lang, task.FileName, position)
	} else {
		statusText = messages.QueueStarted(lang, task.FileName)
	}
	if priority {
		if lang == i18n.RU {
			statusText = statusText + "\n" + "Очередь: приоритетная"
		} else {
			statusText = statusText + "\n" + "Queue: priority"
		}
	}

	if update.CallbackQuery.Message.Message != nil {
		msg := update.CallbackQuery.Message.Message

		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			Text:      statusText,
			ParseMode: messages.ParseModeHTML,
		})
	}

	bh.removePendingSelection(userID, messageID, taskID)
	bh.refreshPendingSelections(ctx, b, userID, lang, unlimited, remaining, messageID, taskID)
	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
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

func (bh *Handlers) answerCallbackAlert(ctx context.Context, b *bot.Bot, callbackID, text string) error {
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
		ShowAlert:       true,
	})
	return err
}
