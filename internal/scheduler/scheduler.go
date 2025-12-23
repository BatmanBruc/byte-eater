package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/converter"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
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
	taskQueue  chan string
	inFlight   map[string]*inFlightEntry
	inFlightMu sync.RWMutex
}

type inFlightEntry struct {
	chatID    int64
	messageID int
	position  int
	fileName  string
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
		store:     store,
		converter: converter,
		botClient: botClient,
		workers:   config.Workers,
		ctx:       ctx,
		cancel:    cancel,
		running:   false,
		taskQueue: make(chan string, queueSize),
		inFlight:  make(map[string]*inFlightEntry),
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
		s.EnqueueTask(task.ID, 0, 0, task.FileName)
		enqueued++
	}

	if enqueued > 0 || skipped > 0 {
		log.Printf("Scheduler recovery: enqueued=%d skipped=%d (processing tasks=%d)", enqueued, skipped, len(tasks))
	}
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

func (s *Scheduler) EnqueueTask(taskID string, chatID int64, messageID int, fileName string) int {
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
	}
	s.inFlightMu.Unlock()

	go func() {
		select {
		case s.taskQueue <- taskID:
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
		select {
		case <-s.ctx.Done():
			log.Printf("Worker %d stopped", id)
			return
		case taskID := <-s.taskQueue:
			log.Println(taskID, id)

			task, err := s.store.GetTask(taskID)
			if err != nil {
				log.Printf("Worker %d: error getting task %s: %v", id, taskID, err)
				s.inFlightMu.Lock()
				delete(s.inFlight, taskID)
				s.inFlightMu.Unlock()
				continue
			}

			if err := s.processTask(task); err != nil {
				log.Printf("Worker %d: error processing task %s: %v", id, taskID, err)
			}

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
				text:      messages.QueueStarted(name),
			})
		} else {
			updates = append(updates, upd{
				chatID:    entry.chatID,
				messageID: entry.messageID,
				text:      messages.QueueQueued(name, entry.position),
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

	resultPath, outName, err := s.converter.Convert(ctx, s.botClient, task.FileID, task.OriginalExt, task.TargetExt, task.FileName)
	if err != nil {
		if err := s.store.SetTaskError(task.ID, err.Error()); err != nil {
			log.Printf("Error setting task error: %v", err)
		}

		s.botClient.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    session.ChatID,
			Text:      messages.ErrorConversionFailed(task.FileName, err),
			ParseMode: messages.ParseModeHTML,
		})

		return err
	}

	msg, err := s.sendDocumentFromPath(ctx, session.ChatID, resultPath, outName)
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

func (s *Scheduler) sendDocumentFromPath(ctx context.Context, chatID int64, filePath string, fileName string) (*models.Message, error) {
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
		Caption: fileName,
	})
	return msg, err
}
