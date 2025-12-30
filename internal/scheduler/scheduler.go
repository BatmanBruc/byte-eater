package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/converter"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/pricing"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Scheduler struct {
	store      types.TaskStore
	converter  converter.Converter
	botClient  *bot.Bot
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    bool
	taskQueueP chan string
	taskQueueN chan string
	inFlight   map[string]*inFlightEntry
	inFlightMu sync.RWMutex
	heavySem   chan struct{}
}

type inFlightEntry struct {
	chatID    int64
	messageID int
	position  int
	fileName  string
	lang      i18n.Lang
}

type Config struct {
	Workers int
}

func NewScheduler(store types.TaskStore, converter converter.Converter, botClient *bot.Bot, config Config) *Scheduler {
	if config.Workers <= 0 {
		config.Workers = 3
	}

	ctx, cancel := context.WithCancel(context.Background())

	queueSize := config.Workers * 2
	if queueSize < 10 {
		queueSize = 10
	}

	return &Scheduler{
		store:      store,
		converter:  converter,
		botClient:  botClient,
		workers:    config.Workers,
		ctx:        ctx,
		cancel:     cancel,
		running:    false,
		taskQueueP: make(chan string, queueSize),
		taskQueueN: make(chan string, queueSize),
		inFlight:   make(map[string]*inFlightEntry),
		heavySem:   make(chan struct{}, 1),
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	log.Printf("Scheduler started with %d workers", s.workers)

	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	go s.recoverProcessingTasks()
}

func (s *Scheduler) recoverProcessingTasks() {
	tasks, err := s.store.GetProcessingTasks()
	if err != nil {
		log.Printf("Scheduler recovery: failed to list processing tasks: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	enqueued := 0
	skipped := 0
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if strings.TrimSpace(task.TargetExt) == "" {
			skipped++
			continue
		}
		s.EnqueueTask(task.ID, 0, 0, task.FileName, langFromTask(task), priorityFromTask(task))
		enqueued++
	}

	if enqueued > 0 || skipped > 0 {
		log.Printf("Scheduler recovery: enqueued=%d skipped=%d (processing tasks=%d)", enqueued, skipped, len(tasks))
	}
}

func priorityFromTask(task *types.Task) bool {
	if task == nil || task.Options == nil {
		return false
	}
	if v, ok := task.Options["priority"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if v, ok := task.Options["unlimited"]; ok {
		if b, ok := v.(bool); ok && b {
			return true
		}
	}
	return false
}

func langFromTask(task *types.Task) i18n.Lang {
	if task == nil || task.Options == nil {
		return i18n.EN
	}
	if v, ok := task.Options["lang"]; ok {
		if s, ok := v.(string); ok {
			return i18n.Parse(s)
		}
	}
	return i18n.EN
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	log.Println("Stopping scheduler...")
	s.cancel()
	s.wg.Wait()
	log.Println("Scheduler stopped")
}

func (s *Scheduler) EnqueueTask(taskID string, chatID int64, messageID int, fileName string, lang i18n.Lang, priority bool) int {
	s.inFlightMu.Lock()
	if _, exists := s.inFlight[taskID]; exists {
		s.inFlightMu.Unlock()
		return -1
	}

	running := 0
	maxPos := 0
	for _, e := range s.inFlight {
		if e == nil {
			continue
		}
		if e.position == 0 {
			running++
			continue
		}
		if e.position > maxPos {
			maxPos = e.position
		}
	}

	position := 0
	if running >= s.workers {
		position = maxPos + 1
	}

	s.inFlight[taskID] = &inFlightEntry{
		chatID:    chatID,
		messageID: messageID,
		position:  position,
		fileName:  fileName,
		lang:      lang,
	}
	s.inFlightMu.Unlock()

	go func() {
		select {
		case func() chan string {
			if priority {
				return s.taskQueueP
			}
			return s.taskQueueN
		}() <- taskID:
		case <-s.ctx.Done():
			s.inFlightMu.Lock()
			delete(s.inFlight, taskID)
			s.inFlightMu.Unlock()
		}
	}()

	return position
}

func (s *Scheduler) worker(id int) {
	defer s.wg.Done()

	log.Printf("Worker %d started", id)

	for {
		var taskID string
		select {
		case <-s.ctx.Done():
			log.Printf("Worker %d stopped", id)
			return
		case taskID = <-s.taskQueueP:
		default:
			select {
			case <-s.ctx.Done():
				log.Printf("Worker %d stopped", id)
				return
			case taskID = <-s.taskQueueP:
			case taskID = <-s.taskQueueN:
			}
		}

		if strings.TrimSpace(taskID) == "" {
			continue
		}

		task, err := s.store.GetTask(taskID)
		if err != nil {
			log.Printf("Worker %d: error getting task %s: %v", id, taskID, err)
			s.inFlightMu.Lock()
			delete(s.inFlight, taskID)
			s.inFlightMu.Unlock()
			continue
		}

		isHeavy := s.isHeavyTask(task)
		if isHeavy {
			select {
			case s.heavySem <- struct{}{}:
			case <-s.ctx.Done():
				s.inFlightMu.Lock()
				delete(s.inFlight, taskID)
				s.inFlightMu.Unlock()
				return
			}
		}

		func() {
			defer func() {
				if isHeavy {
					<-s.heavySem
				}
			}()

			if err := s.processTask(task); err != nil {
				log.Printf("Worker %d: error processing task %s: %v", id, taskID, err)
			}
		}()

		var entry *inFlightEntry
		s.inFlightMu.RLock()
		entry = s.inFlight[taskID]
		s.inFlightMu.RUnlock()
		if entry != nil && entry.chatID != 0 && entry.messageID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := s.botClient.DeleteMessage(ctx, &bot.DeleteMessageParams{
				ChatID:    entry.chatID,
				MessageID: entry.messageID,
			})
			cancel()
			if err != nil {
				log.Printf("Failed to delete status message chat=%d msg=%d: %v", entry.chatID, entry.messageID, err)
			}
		}

		s.inFlightMu.Lock()
		delete(s.inFlight, taskID)
		s.inFlightMu.Unlock()

		go s.decrementQueueAndUpdateMessages()
	}
}

func (s *Scheduler) isHeavyTask(task *types.Task) bool {
	if task == nil {
		return false
	}

	fileSize := int64(0)
	if task.Options != nil {
		if v, ok := task.Options["is_heavy"]; ok {
			switch t := v.(type) {
			case bool:
				return t
			case string:
				return strings.EqualFold(strings.TrimSpace(t), "true")
			}
		}
		if v, ok := task.Options["file_size"]; ok {
			switch t := v.(type) {
			case int64:
				fileSize = t
			case int:
				fileSize = int64(t)
			case float64:
				fileSize = int64(t)
			case string:
				if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
					fileSize = n
				}
			}
		}
	}

	_, heavy := pricing.Credits(task.OriginalExt, task.TargetExt, fileSize)
	return heavy
}

func (s *Scheduler) decrementQueueAndUpdateMessages() {
	type upd struct {
		chatID    int64
		messageID int
		text      string
	}
	updates := make([]upd, 0)

	s.inFlightMu.Lock()
	for _, entry := range s.inFlight {
		if entry == nil {
			continue
		}

		if entry.position == 0 {
			continue
		}

		entry.position--

		if entry.chatID == 0 || entry.messageID == 0 {
			continue
		}

		name := strings.TrimSpace(entry.fileName)
		if name == "" {
			name = "файл"
		}

		if entry.position == 0 {
			updates = append(updates, upd{
				chatID:    entry.chatID,
				messageID: entry.messageID,
				text:      messages.QueueStarted(entry.lang, name),
			})
		} else {
			updates = append(updates, upd{
				chatID:    entry.chatID,
				messageID: entry.messageID,
				text:      messages.QueueQueued(entry.lang, name, entry.position),
			})
		}
	}
	s.inFlightMu.Unlock()

	if len(updates) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, u := range updates {
		_, err := s.botClient.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    u.chatID,
			MessageID: u.messageID,
			Text:      u.text,
			ParseMode: messages.ParseModeHTML,
		})
		if err != nil {
			log.Printf("Queue update: failed to edit message chat=%d msg=%d: %v", u.chatID, u.messageID, err)
		}
	}
}

func (s *Scheduler) processTask(task *types.Task) error {
	log.Printf("Processing task %s: %s -> %s", task.ID, task.OriginalExt, task.TargetExt)

	session, err := s.store.GetSession(task.SessionID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	lang := langFromTask(task)

	resultPath, outName, err := s.converter.Convert(ctx, s.botClient, task.FileID, task.OriginalExt, task.TargetExt, task.FileName, task.Options)
	if err != nil {
		if err := s.store.SetTaskError(task.ID, err.Error()); err != nil {
			log.Printf("Error setting task error: %v", err)
		}

		s.botClient.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    session.ChatID,
			Text:      messages.ErrorConversionFailed(lang, task.FileName, err),
			ParseMode: messages.ParseModeHTML,
		})

		return err
	}

	caption := s.resultCaption(task, outName)
	msg, err := s.sendDocumentFromPath(ctx, session.ChatID, resultPath, outName, caption)
	if err != nil {
		log.Printf("Error sending document: %v", err)
		_ = os.Remove(resultPath)
		_ = s.store.SetTaskError(task.ID, fmt.Sprintf("send document failed: %v", err))
		return err
	}

	resultFileID := ""
	if msg != nil && msg.Document != nil {
		resultFileID = msg.Document.FileID
	}

	if err := s.store.SetTaskReady(task.ID, resultFileID); err != nil {
		log.Printf("Error setting ready for task %s: %v", task.ID, err)

		return err
	}

	if err := os.Remove(resultPath); err != nil {
		log.Printf("Error removing result file %s: %v", resultPath, err)
	}

	log.Printf("Task %s completed successfully", task.ID)
	return nil
}

func (s *Scheduler) resultCaption(task *types.Task, fileName string) string {
	caption := strings.TrimSpace(fileName)
	if caption == "" {
		caption = "result"
	}
	if task == nil || task.Options == nil {
		return caption
	}
	lang := langFromTask(task)

	unlimited := false
	if v, ok := task.Options["unlimited"]; ok {
		if b, ok := v.(bool); ok {
			unlimited = b
		}
	}
	if unlimited {
		return caption + "\n\n" + messages.PlanUnlimitedLine(lang)
	}

	rem := 0
	ok := false
	if v, exists := task.Options["credits_remaining"]; exists {
		switch t := v.(type) {
		case int:
			rem = t
			ok = true
		case int64:
			rem = int(t)
			ok = true
		case float64:
			rem = int(t)
			ok = true
		}
	}
	if ok {
		return caption + "\n\n" + messages.CreditsRemainingLine(lang, rem)
	}
	return caption
}

func (s *Scheduler) sendDocumentFromPath(ctx context.Context, chatID int64, filePath string, fileName string, caption string) (*models.Message, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if strings.TrimSpace(fileName) == "" {
		fileName = filepath.Base(filePath)
	}

	msg, err := s.botClient.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: fileName,
			Data:     file,
		},
		Caption: caption,
	})
	return msg, err
}
