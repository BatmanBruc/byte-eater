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
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) startManualBatchTimer(b *bot.Bot, userID int64) {
	key := fmt.Sprintf("mb:%d", userID)
	bh.batchMu.Lock()
	if t, ok := bh.batchTimers[key]; ok && t != nil {
		t.Stop()
	}
	bh.batchTimers[key] = time.AfterFunc(10*time.Second, func() {
		bh.manualBatchFinalize(b, userID, true)
	})
	bh.batchMu.Unlock()
}

func (bh *Handlers) resetManualBatchTimer(b *bot.Bot, userID int64) {
	key := fmt.Sprintf("mb:%d", userID)
	bh.batchMu.Lock()
	if t, ok := bh.batchTimers[key]; ok && t != nil {
		t.Stop()
	}
	bh.batchTimers[key] = time.AfterFunc(10*time.Second, func() {
		bh.manualBatchFinalize(b, userID, true)
	})
	bh.batchMu.Unlock()
}

func (bh *Handlers) stopManualBatchTimer(userID int64) {
	key := fmt.Sprintf("mb:%d", userID)
	bh.batchMu.Lock()
	if t, ok := bh.batchTimers[key]; ok && t != nil {
		t.Stop()
	}
	delete(bh.batchTimers, key)
	bh.batchMu.Unlock()
}

func (bh *Handlers) manualBatchAddFiles(ctx context.Context, b *bot.Bot, userID int64, lang i18n.Lang, files []contextkeys.FileInfo) {
	if len(files) == 0 {
		return
	}
	if b == nil {
		return
	}
	options, _ := bh.userState.GetUserOptions(userID)
	if options == nil {
		options = map[string]interface{}{}
	}
	expected := 0
	if v, ok := options["mb_expected"]; ok {
		switch t := v.(type) {
		case int:
			expected = t
		case int64:
			expected = int(t)
		case float64:
			expected = int(t)
		}
	}
	list := []interface{}{}
	if v, ok := options["mb_files"]; ok {
		if arr, ok := v.([]interface{}); ok {
			list = arr
		}
	}
	filesBefore := len(list)

	for _, fi := range files {
		list = append(list, map[string]interface{}{
			"file_id":   fi.FileID,
			"file_name": fi.FileName,
			"file_size": fi.FileSize,
		})
	}
	options["mb_files"] = list
	_ = bh.userState.SetUserOptions(userID, options)

	if expected > 0 && len(list) >= expected {
		bh.stopManualBatchTimer(userID)
		bh.manualBatchFinalize(b, userID, false)
	} else if filesBefore == 0 && len(list) > 0 {
		bh.startManualBatchTimer(b, userID)
	} else if len(list) > filesBefore {
		bh.resetManualBatchTimer(b, userID)
	}
	_ = lang
	_ = ctx
}

func (bh *Handlers) manualBatchFinalize(b *bot.Bot, userID int64, timedOut bool) {
	bh.stopManualBatchTimer(userID)
	if b == nil {
		return
	}
	options, _ := bh.userState.GetUserOptions(userID)
	if options == nil {
		return
	}
	lang := i18n.EN
	if v, ok := options["lang"]; ok {
		if s, ok := v.(string); ok {
			lang = i18n.Parse(s)
		}
	}
	expected := 0
	if v, ok := options["mb_expected"]; ok {
		switch t := v.(type) {
		case int:
			expected = t
		case int64:
			expected = int(t)
		case float64:
			expected = int(t)
		}
	}
	files := parseBatchFiles(options["mb_files"])

	delete(options, "mb_state")
	delete(options, "mb_expected")
	delete(options, "mb_files")
	_ = bh.userState.SetUserOptions(userID, options)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chatID := userID
	if timedOut && expected > 0 {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.BatchTimeout(lang, len(files), expected),
			ParseMode: messages.ParseModeHTML,
		})
	}

	if len(files) == 0 {
		return
	}

	order, byExt := bh.groupBatchFilesByExt(files)
	bh.dispatchBatchGroups(ctx, b, chatID, userID, lang, order, byExt)
}

func (bh *Handlers) dispatchBatchGroups(ctx context.Context, b *bot.Bot, chatID int64, userID int64, lang i18n.Lang, order []string, byExt map[string][]formats.BatchFile) {
	for _, extKey := range order {
		groupFiles := byExt[extKey]
		if len(groupFiles) > 1 && extKey != "_unknown_" {
			targets := formats.GetTargetFormatsForSourceExt(extKey)
			if len(targets) > 0 {
				bt := bh.createBatchCollectorTask(userID, lang, extKey, groupFiles)
				_ = bh.store.CreateTask(bt)
				bh.sendBatchChoiceMessage(ctx, b, chatID, lang, extKey, groupFiles, bt.ID)
				continue
			}
		}

		for _, f := range groupFiles {
			bh.createAndAskFormatForSingleFile(ctx, b, userID, lang, f)
		}
	}
}

func parseBatchFiles(v interface{}) []formats.BatchFile {
	if v == nil {
		return nil
	}
	out := make([]formats.BatchFile, 0)
	switch t := v.(type) {
	case []formats.BatchFile:
		return t
	case []interface{}:
		for _, it := range t {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			id, _ := m["file_id"].(string)
			name, _ := m["file_name"].(string)
			size := int64(0)
			if sv, ok := m["file_size"]; ok {
				switch st := sv.(type) {
				case int64:
					size = st
				case int:
					size = int64(st)
				case float64:
					size = int64(st)
				}
			}
			id = strings.TrimSpace(id)
			name = strings.TrimSpace(name)
			if id == "" {
				continue
			}
			out = append(out, formats.BatchFile{FileID: id, FileName: name, FileSize: size})
		}
	}
	return out
}

func batchExtKey(fileName string) string {
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fileName), "."))
	if ext == "" {
		return "_unknown_"
	}
	return ext
}

func (bh *Handlers) groupBatchFilesByExt(files []formats.BatchFile) (order []string, byExt map[string][]formats.BatchFile) {
	byExt = map[string][]formats.BatchFile{}
	order = make([]string, 0)
	for _, f := range files {
		ext := batchExtKey(bh.getExtensionFromFileName(f.FileName))
		if _, ok := byExt[ext]; !ok {
			order = append(order, ext)
		}
		byExt[ext] = append(byExt[ext], f)
	}
	return order, byExt
}

func batchFilesToMaps(files []formats.BatchFile) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]interface{}{
			"file_id":   f.FileID,
			"file_name": f.FileName,
			"file_size": f.FileSize,
		})
	}
	return out
}

func (bh *Handlers) createBatchCollectorTask(userID int64, lang i18n.Lang, extKey string, groupFiles []formats.BatchFile) *types.Task {
	return &types.Task{
		UserID:      userID,
		State:       types.StateChooseExt,
		FileID:      groupFiles[0].FileID,
		FileName:    fmt.Sprintf("%d files.%s", len(groupFiles), extKey),
		OriginalExt: extKey,
		TargetExt:   "",
		Options: map[string]interface{}{
			"lang":        string(lang),
			"batch_mode":  "",
			"batch_files": batchFilesToMaps(groupFiles),
		},
	}
}

func (bh *Handlers) sendBatchChoiceMessage(ctx context.Context, b *bot.Bot, chatID int64, lang i18n.Lang, extKey string, groupFiles []formats.BatchFile, taskID string) {
	rows := [][]models.InlineKeyboardButton{
		{
			{Text: " " + messages.BatchBtnAll(lang) + " ", CallbackData: "batch_all_for_" + taskID},
		},
		{
			{Text: " " + messages.BatchBtnSeparate(lang) + " ", CallbackData: "batch_sep_for_" + taskID},
		},
	}
	text := messages.BatchReceivedChoice(lang, extKey, len(groupFiles))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: messages.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: rows,
		},
	})
}
