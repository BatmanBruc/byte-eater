package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) HandleText(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	if update == nil || update.Message == nil {
		return
	}
	lang := bh.langFromUserOrCtx(ctx, userID)
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.EmptyTextHint(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	options, _ := bh.userState.GetUserOptions(userID)
	if options != nil {
		if st, ok := options["mb_state"].(string); ok && strings.TrimSpace(st) == "await_count" {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || n <= 0 || n > 100 {
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:    chatID,
					Text:      messages.BatchCountInvalid(lang),
					ParseMode: messages.ParseModeHTML,
				})
				return
			}
			options["mb_state"] = "collect"
			options["mb_expected"] = n
			options["mb_files"] = []interface{}{}
			_ = bh.userState.SetUserOptions(userID, options)
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.BatchCountAccepted(lang, n),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
	}

	tmpDir := filepath.Join(os.TempDir(), "bot_converter_text")
	_ = os.MkdirAll(tmpDir, 0755)
	tmpName := fmt.Sprintf("text_%d.txt", time.Now().Unix())
	tmpPath := filepath.Join(tmpDir, tmpName)

	if err := os.WriteFile(tmpPath, []byte(text), 0644); err != nil {
		log.Printf("Error writing temp text file: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorDefault(lang),
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
			Text:      messages.ErrorDefault(lang),
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
			Text:      messages.ErrorUploadTextAsFile(lang),
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

	fileSize := int64(0)
	if msg.Document != nil {
		fileSize = int64(msg.Document.FileSize)
	}
	task, err := bh.store.SetProcessingFile(userID, fileID, fileName, fileSize)
	if err != nil {
		log.Printf("Error creating task for text file: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.ErrorDefault(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}
	if task.Options == nil {
		task.Options = map[string]interface{}{}
	}
	task.Options["lang"] = string(lang)
	task.Options["text_input"] = true
	_ = bh.store.UpdateTask(task)

	buttons := formats.GetTextOutputButtons(task.ID)
	keyboard := utils.BuildInlineKeyboard(buttons)
	textOut := messages.TextReceivedChooseFormat(lang)
	if bh.billing != nil {
		unlimited, err := bh.billing.IsUnlimited(userID)
		if err == nil && unlimited {
			textOut = textOut + "\n\n" + messages.PlanUnlimitedLine(lang)
		}
	}
	sent, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        textOut,
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
	if sent != nil {
		bh.addPendingSelection(userID, sent.ID, task.ID)
	}
}
