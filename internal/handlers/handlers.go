package handlers

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type TaskEnqueuer interface {
	EnqueueTask(taskID string, chatID int64, messageID int, fileName string, lang i18n.Lang, priority bool) int
}

type Handlers struct {
	store     types.TaskStore
	userState types.UserStateStore
	scheduler TaskEnqueuer
	userStore types.UserStore
	billing   types.BillingStore

	batchMu     sync.Mutex
	batchTimers map[string]*time.Timer
	batchTaskID map[string]string
}

func getChatIDFromUpdate(update *models.Update) int64 {
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

func (bh *Handlers) langFromUserOrCtx(ctx context.Context, userID int64) i18n.Lang {
	if bh.userState != nil {
		options, err := bh.userState.GetUserOptions(userID)
		if err == nil && options != nil {
			if v, ok := options["lang"]; ok {
				if s, ok := v.(string); ok {
					return i18n.Parse(s)
				}
			}
		}
	}
	if v, ok := contextkeys.GetLang(ctx); ok {
		return i18n.Parse(v)
	}
	return i18n.EN
}

func NewHandlers(store types.TaskStore, userState types.UserStateStore, scheduler TaskEnqueuer, userStore types.UserStore, billing types.BillingStore) *Handlers {
	return &Handlers{
		store:       store,
		userState:   userState,
		scheduler:   scheduler,
		userStore:   userStore,
		billing:     billing,
		batchTimers: make(map[string]*time.Timer),
		batchTaskID: make(map[string]string),
	}
}

func (bh *Handlers) MainHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatIDFromUpdate(update)
	userID, is := contextkeys.GetUserID(ctx)
	if !is {
		log.Printf("Error: UserID not found")
		if chatID != 0 {
			lang := i18n.EN
			if v, ok := contextkeys.GetLang(ctx); ok {
				lang = i18n.Parse(v)
			}
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorDefault(lang),
				ParseMode: messages.ParseModeHTML,
			})
		}
		return
	}

	messageType, _ := contextkeys.GetMessageType(ctx)
	lang := bh.langFromUserOrCtx(ctx, userID)

	switch messageType {
	case contextkeys.MessageTypeCommand:
		bh.HandleCommand(ctx, b, update, userID)
	case contextkeys.MessageTypeDocument, contextkeys.MessageTypePhoto, contextkeys.MessageTypeVideo,
		contextkeys.MessageTypeAudio, contextkeys.MessageTypeVoice:
		bh.HandleFile(ctx, b, update, userID)
	case contextkeys.MessageTypeText:
		bh.HandleText(ctx, b, update, userID)
	case contextkeys.MessageTypeClickButton:
		data, _ := contextkeys.GetCallbackData(ctx)
		if data == "" && update.CallbackQuery != nil {
			data = update.CallbackQuery.Data
		}
		if strings.HasPrefix(strings.TrimSpace(data), "menu_") {
			bh.HandleMenuClick(ctx, b, update, userID)
		} else {
			bh.HandleClickButton(ctx, b, update, userID)
		}
	case contextkeys.MessageTypePreCheckout:
		bh.HandlePreCheckout(ctx, b, update, userID)
	case contextkeys.MessageTypePayment:
		bh.HandleSuccessfulPayment(ctx, b, update, userID)
	default:
		if chatID != 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorUnsupportedMessageType(lang),
				ParseMode: messages.ParseModeHTML,
			})
		}
	}
}
