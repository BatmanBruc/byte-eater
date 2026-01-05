package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/sync/errgroup"
)

func (bh *Handlers) handleMergePDFFile(ctx context.Context, b *bot.Bot, userID int64, lang i18n.Lang, files []contextkeys.FileInfo) {
	if len(files) == 0 {
		return
	}

	options, _ := bh.userState.GetUserOptions(userID)
	if options == nil {
		options = map[string]interface{}{}
	}

	chatID := userID

	for _, fi := range files {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fi.FileName), "."))
		if ext != "pdf" {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorUnsupportedFormat(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
	}

	list := []interface{}{}
	if v, ok := options["merge_files"]; ok {
		if arr, ok := v.([]interface{}); ok {
			list = arr
		}
	}

	for _, fi := range files {
		list = append(list, map[string]interface{}{
			"file_id":   fi.FileID,
			"file_name": fi.FileName,
			"file_size": fi.FileSize,
		})
	}
	options["merge_files"] = list

	var oldMsgID int
	if msgID, ok := options["merge_msg_id"]; ok {
		switch v := msgID.(type) {
		case int:
			oldMsgID = v
		case int64:
			oldMsgID = int(v)
		case float64:
			oldMsgID = int(v)
		}
		if oldMsgID > 0 {
			log.Printf("Deleting previous merge message: %d", oldMsgID)
			_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
				ChatID:    chatID,
				MessageID: oldMsgID,
			})
			if err != nil {
				log.Printf("Error deleting message %d: %v", oldMsgID, err)
			} else {
				log.Printf("Successfully deleted message %d", oldMsgID)
			}
		}
	}

	_ = bh.userState.SetUserOptions(userID, options)

	fileNames := []string{}
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["file_name"].(string); ok {
				fileNames = append(fileNames, name)
			}
		}
	}

	sent, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      messages.MergePDFFilesList(lang, fileNames),
		ParseMode: messages.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: messages.MergePDFBtn(lang), CallbackData: "merge_pdf"},
				},
			},
		},
	})

	if err == nil && sent != nil {
		log.Printf("Saved new merge message ID: %d", sent.ID)
		options["merge_msg_id"] = sent.ID
		_ = bh.userState.SetUserOptions(userID, options)
	} else if err != nil {
		log.Printf("Error sending merge message: %v", err)
	}
}

func (bh *Handlers) handleMergePDF(ctx context.Context, b *bot.Bot, update *models.Update, userID int64, lang i18n.Lang) {
	options, _ := bh.userState.GetUserOptions(userID)
	if options == nil {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotFound(lang))
		return
	}

	files := []string{}
	if v, ok := options["merge_files"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if name, ok := m["file_name"].(string); ok {
						files = append(files, name)
					}
				}
			}
		}
	}

	if len(files) < 2 {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidAction(lang))
		return
	}

	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")

	chatID := getChatIDFromUpdate(update)
	if chatID == 0 {
		chatID = userID
	}

	if msgID, ok := options["merge_msg_id"]; ok {
		if id, ok := msgID.(int); ok {
			log.Printf("Deleting merge message after button click: %d", id)
			_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
				ChatID:    chatID,
				MessageID: id,
			})
			if err != nil {
				log.Printf("Error deleting merge message %d: %v", id, err)
			}
		}
	}

	fileInfos := []contextkeys.FileInfo{}
	if v, ok := options["merge_files"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					fileID, _ := m["file_id"].(string)
					fileName, _ := m["file_name"].(string)
					fileSize, _ := m["file_size"].(float64)
					fileInfos = append(fileInfos, contextkeys.FileInfo{
						FileID:   fileID,
						FileName: fileName,
						FileSize: int64(fileSize),
					})
				}
			}
		}
	}

	delete(options, "merge_state")
	delete(options, "merge_files")
	delete(options, "merge_msg_id")
	_ = bh.userState.SetUserOptions(userID, options)

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      messages.MergePDFStarted(lang),
		ParseMode: messages.ParseModeHTML,
	})

	go bh.processMergePDF(b, userID, chatID, lang, fileInfos)
}

func (bh *Handlers) downloadFile(ctx context.Context, client *http.Client, b *bot.Bot, fileID, tempDir, fileName string) (string, error) {
	fileInfo, err := b.GetFile(ctx, &bot.GetFileParams{
		FileID: fileID,
	})
	if err != nil {
		return "", fmt.Errorf("error getting file info: %v", err)
	}

	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.Token(), fileInfo.FilePath)

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return "", err
	}

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	destPath := filepath.Join(tempDir, fmt.Sprintf("%s_%d_%s", fileID, time.Now().UnixNano(), fileName))
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(destPath)
		return "", err
	}

	return destPath, nil
}

func (bh *Handlers) processMergePDF(b *bot.Bot, userID int64, chatID int64, lang i18n.Lang, fileInfos []contextkeys.FileInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	log.Printf("Starting PDF merge for user %d with %d files", userID, len(fileInfos))

	if len(fileInfos) < 2 {
		log.Printf("Not enough files to merge: %d", len(fileInfos))
		return
	}

	tempDir := strings.TrimSpace(os.Getenv("TMPDIR"))
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	_ = os.MkdirAll(tempDir, 0755)
	pdfPaths := make([]string, len(fileInfos))
	defer func() {
		for _, path := range pdfPaths {
			if strings.TrimSpace(path) != "" {
				_ = os.Remove(path)
			}
		}
	}()

	downloadClient := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			MaxConnsPerHost:       10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	g, gctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, 3)
	for i, fi := range fileInfos {
		i, fi := i, fi
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			safeName := fmt.Sprintf("merge_%03d.pdf", i+1)
			log.Printf("Downloading file: %s (ID: %s)", fi.FileName, fi.FileID)

			start := time.Now()
			path, err := bh.downloadFile(gctx, downloadClient, b, fi.FileID, tempDir, safeName)
			if err != nil {
				return fmt.Errorf("download %q failed: %w", fi.FileName, err)
			}
			pdfPaths[i] = path

			if st, err2 := os.Stat(path); err2 == nil {
				log.Printf("Downloaded file to: %s (size=%d bytes, took=%s)", path, st.Size(), time.Since(start))
			} else {
				log.Printf("Downloaded file to: %s (took=%s)", path, time.Since(start))
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		log.Printf("Error downloading PDFs: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFError(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	log.Printf("Downloaded %d files, starting merge", len(pdfPaths))

	for i, path := range pdfPaths {
		stat, err := os.Stat(path)
		if os.IsNotExist(err) {
			log.Printf("PDF file %d does not exist: %s", i, path)
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.MergePDFError(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		if err != nil {
			log.Printf("Error checking PDF file %d: %v", i, err)
			return
		}
		log.Printf("PDF file %d exists: %s (size: %d bytes)", i, path, stat.Size())
	}

	outputPath := filepath.Join(tempDir, "merged_"+time.Now().Format("20060102_150405")+".pdf")
	log.Printf("Output path: %s", outputPath)

	var cmd *exec.Cmd
	var toolName string

	if _, err := exec.LookPath("pdftk"); err == nil {
		args := append(pdfPaths, "cat", "output", outputPath)
		cmd = exec.Command("pdftk", args...)
		toolName = "pdftk"
		log.Printf("Using pdftk with args: %v", args)
	} else if _, err := exec.LookPath("qpdf"); err == nil {
		args := []string{"--empty", "--pages"}
		for _, path := range pdfPaths {
			args = append(args, path, "1-z")
		}
		args = append(args, "--", outputPath)
		cmd = exec.Command("qpdf", args...)
		toolName = "qpdf"
		log.Printf("Using qpdf with args: %v", args)
	} else {
		log.Printf("Neither pdftk nor qpdf is available to merge PDFs")
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFError(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	output, err := cmd.CombinedOutput()
	log.Printf("%s command completed, output: %s", toolName, string(output))
	if err != nil {
		log.Printf("Error merging PDFs with %s: %v", toolName, err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFError(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		log.Printf("Output file does not exist: %s", outputPath)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFError(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	defer os.Remove(outputPath)

	log.Printf("Sending merged PDF: %s", outputPath)
	file, err := os.Open(outputPath)
	if err != nil {
		log.Printf("Error opening merged PDF: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFError(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.Printf("Error getting file stat: %v", err)
		return
	}
	log.Printf("Merged PDF size: %d bytes", stat.Size())

	_, err = b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: "merged.pdf",
			Data:     file,
		},
		Caption:   messages.MergePDFSuccess(lang),
		ParseMode: messages.ParseModeHTML,
	})

	if err != nil {
		log.Printf("Error sending merged PDF: %v", err)
	} else {
		log.Printf("Successfully sent merged PDF to user %d", userID)
	}
}
