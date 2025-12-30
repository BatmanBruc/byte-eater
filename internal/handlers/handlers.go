package handlers

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

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/pricing"
	"github.com/BatmanBruc/bat-bot-convetor/store"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type TaskEnqueuer interface {
	EnqueueTask(taskID string, chatID int64, messageID int, fileName string, lang i18n.Lang, priority bool) int
}

type Handlers struct {
	store     types.TaskStore
	scheduler TaskEnqueuer
	userStore types.UserStore
	billing   types.BillingStore

	batchMu     sync.Mutex
	batchTimers map[string]*time.Timer
	batchTaskID map[string]string
}

func langFromSessionOrCtx(ctx context.Context, session *types.Session) i18n.Lang {
	if session != nil && session.Options != nil {
		if v, ok := session.Options["lang"]; ok {
			if s, ok := v.(string); ok {
				return i18n.Parse(s)
			}
		}
	}
	if v, ok := contextkeys.GetLang(ctx); ok {
		return i18n.Parse(v)
	}
	return i18n.EN
}

func NewHandlers(store types.TaskStore, scheduler TaskEnqueuer, userStore types.UserStore, billing types.BillingStore) *Handlers {
	return &Handlers{
		store:       store,
		scheduler:   scheduler,
		userStore:   userStore,
		billing:     billing,
		batchTimers: make(map[string]*time.Timer),
		batchTaskID: make(map[string]string),
	}
}

func (bh *Handlers) MainHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := bh.getChatIDFromUpdate(update)
	messageType, _ := contextkeys.GetMessageType(ctx)
	lang := i18n.EN
	if v, ok := contextkeys.GetLang(ctx); ok {
		lang = i18n.Parse(v)
	}

	sessionID, ok := contextkeys.GetSessionID(ctx)
	if !ok {
		log.Printf("Error: SessionID not found in context")
		if chatID != 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      messages.ErrorDefault(lang),
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
				Text:      messages.ErrorDefault(lang),
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
		data, _ := contextkeys.GetCallbackData(ctx)
		if data == "" && update.CallbackQuery != nil {
			data = update.CallbackQuery.Data
		}
		if strings.HasPrefix(strings.TrimSpace(data), "menu_") {
			bh.HandleMenuClick(ctx, b, update, session)
		} else {
			bh.HandleClickButton(ctx, b, update, session)
		}
	case contextkeys.MessageTypePreCheckout:
		bh.HandlePreCheckout(ctx, b, update, session)
	case contextkeys.MessageTypePayment:
		bh.HandleSuccessfulPayment(ctx, b, update, session)
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

func (bh *Handlers) HandlePreCheckout(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	if update == nil || update.PreCheckoutQuery == nil {
		return
	}
	lang := langFromSessionOrCtx(ctx, session)
	payload := strings.TrimSpace(update.PreCheckoutQuery.InvoicePayload)
	expected := strings.TrimSpace(os.Getenv("SUB_PAYLOAD"))
	if expected == "" {
		expected = "sub_unlimited_month"
	}
	ok := payload == expected
	_, _ = b.AnswerPreCheckoutQuery(ctx, &bot.AnswerPreCheckoutQueryParams{
		PreCheckoutQueryID: update.PreCheckoutQuery.ID,
		OK:                 ok,
		ErrorMessage: func() string {
			if ok {
				return ""
			}
			if lang == i18n.RU {
				return "Некорректный платеж"
			}
			return "Invalid payment"
		}(),
	})
}

func (bh *Handlers) HandleSuccessfulPayment(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	if update == nil || update.Message == nil || update.Message.SuccessfulPayment == nil {
		return
	}
	lang := langFromSessionOrCtx(ctx, session)
	p := update.Message.SuccessfulPayment
	payload := strings.TrimSpace(p.InvoicePayload)
	expected := strings.TrimSpace(os.Getenv("SUB_PAYLOAD"))
	if expected == "" {
		expected = "sub_unlimited_month"
	}
	if payload != expected {
		return
	}
	if bh.userStore == nil {
		return
	}
	inserted, err := bh.userStore.RecordPayment(types.Payment{
		UserID: session.UserID,
		Provider: func() string {
			if strings.EqualFold(strings.TrimSpace(p.Currency), "XTR") {
				return "stars"
			}
			return "yookassa"
		}(),
		Currency:              strings.TrimSpace(p.Currency),
		TotalAmount:           int64(p.TotalAmount),
		InvoicePayload:        payload,
		TelegramPaymentCharge: strings.TrimSpace(p.TelegramPaymentChargeID),
		ProviderPaymentCharge: strings.TrimSpace(p.ProviderPaymentChargeID),
		CreatedAt:             time.Now().UTC(),
	})
	if err != nil {
		return
	}
	if !inserted {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    session.ChatID,
			Text:      messages.PaymentAlreadyProcessed(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	sub, err := bh.userStore.ActivateOrExtendUnlimited(session.UserID, 30*24*time.Hour)
	if err != nil {
		return
	}
	until := time.Now().UTC()
	if sub != nil && sub.ExpiresAt != nil {
		until = sub.ExpiresAt.UTC()
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    session.ChatID,
		Text:      messages.PaymentSucceeded(lang, until),
		ParseMode: messages.ParseModeHTML,
	})
}

func (bh *Handlers) buildMenuKeyboard(lang i18n.Lang, withBack bool) models.InlineKeyboardMarkup {
	pad := func(s string) string { return "   " + s + "   " }
	rows := make([][]models.InlineKeyboardButton, 0, 4)
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: pad(messages.MenuBtnSubscription(lang)), CallbackData: "menu_sub"},
	})
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: pad(messages.MenuBtnContact(lang)), URL: "https://t.me/esteticcus"},
	})
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: pad(messages.MenuBtnAbout(lang)), CallbackData: "menu_about"},
	})
	if withBack {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: pad(messages.MenuBtnBack(lang)), CallbackData: "menu_back"},
		})
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (bh *Handlers) sendMainMenu(ctx context.Context, b *bot.Bot, chatID int64, lang i18n.Lang) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      messages.MainMenuText(lang),
		ParseMode: messages.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: bh.buildMenuKeyboard(lang, false).InlineKeyboard,
		},
	})
}

func (bh *Handlers) HandleMenuClick(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	lang := langFromSessionOrCtx(ctx, session)
	data, _ := contextkeys.GetCallbackData(ctx)
	if data == "" {
		data = update.CallbackQuery.Data
	}
	data = strings.TrimSpace(data)

	if update.CallbackQuery.Message.Message == nil {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		return
	}
	msg := update.CallbackQuery.Message.Message

	text := messages.MainMenuText(lang)
	keyboard := bh.buildMenuKeyboard(lang, false)

	switch data {
	case "menu_sub":
		active := false
		var expiresAt *time.Time
		if bh.userStore != nil {
			sub, err := bh.userStore.GetSubscription(session.UserID)
			if err == nil && sub != nil {
				if strings.EqualFold(strings.TrimSpace(sub.Status), "active") && strings.EqualFold(strings.TrimSpace(sub.Plan), "unlimited") {
					if sub.ExpiresAt == nil || sub.ExpiresAt.After(time.Now()) {
						active = true
						expiresAt = sub.ExpiresAt
					}
				}
			}
		} else if bh.billing != nil {
			u, _ := bh.billing.IsUnlimited(session.UserID)
			active = u
		}

		btnPad := func(s string) string { return "   " + s + "   " }
		if active {
			text = messages.SubscriptionActiveDetails(lang, expiresAt)
			rows := [][]models.InlineKeyboardButton{
				{
					{Text: btnPad(messages.MenuBtnSubscribeNow(lang, true)), CallbackData: "menu_pay"},
				},
				{
					{Text: btnPad(messages.MenuBtnBack(lang)), CallbackData: "menu_back"},
				},
			}
			keyboard = models.InlineKeyboardMarkup{InlineKeyboard: rows}
		} else {
			text = messages.SubscriptionOffer(lang)
			rows := [][]models.InlineKeyboardButton{
				{
					{Text: btnPad(messages.MenuBtnSubscribeNow(lang, false)), CallbackData: "menu_pay"},
				},
				{
					{Text: btnPad(messages.MenuBtnBack(lang)), CallbackData: "menu_back"},
				},
			}
			keyboard = models.InlineKeyboardMarkup{InlineKeyboard: rows}
		}
	case "menu_pay":
		text = messages.PayMethodTitle(lang)
		btnPad := func(s string) string { return "   " + s + "   " }
		rows := [][]models.InlineKeyboardButton{
			{
				{Text: btnPad(messages.PayBtnStars(lang)), CallbackData: "menu_pay_stars"},
			},
			{
				{Text: btnPad(messages.PayBtnYooKassa(lang)), CallbackData: "menu_pay_yk"},
			},
			{
				{Text: btnPad(messages.MenuBtnBack(lang)), CallbackData: "menu_sub"},
			},
		}
		keyboard = models.InlineKeyboardMarkup{InlineKeyboard: rows}
	case "menu_pay_stars":
		ok := bh.sendSubscriptionInvoiceStars(ctx, b, msg.Chat.ID, lang)
		if ok {
			_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.PaymentCreated(lang))
		} else {
			_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.PaymentNotConfigured(lang))
		}
		return
	case "menu_pay_yk":
		ok := bh.sendSubscriptionInvoiceYooKassa(ctx, b, msg.Chat.ID, lang)
		if ok {
			_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.PaymentCreated(lang))
		} else {
			_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.PaymentNotConfigured(lang))
		}
		return
	case "menu_about":
		text = formats.GetHelpMessage(lang) + "\n\n" + messages.AboutCreditsBlock(lang)
		keyboard = bh.buildMenuKeyboard(lang, true)
	case "menu_back":
	default:
	}

	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		Text:      text,
		ParseMode: messages.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard.InlineKeyboard,
		},
	})
}

func getEnvInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (bh *Handlers) sendSubscriptionInvoiceStars(ctx context.Context, b *bot.Bot, chatID int64, lang i18n.Lang) bool {
	priceStars := getEnvInt("SUB_PRICE_STARS", 150)
	payload := strings.TrimSpace(os.Getenv("SUB_PAYLOAD"))
	if payload == "" {
		payload = "sub_unlimited_month"
	}
	_, err := b.SendInvoice(ctx, &bot.SendInvoiceParams{
		ChatID:         chatID,
		Title:          "Unlimited subscription",
		Description:    "Unlimited conversions for 1 month",
		Payload:        payload,
		Currency:       "XTR",
		Prices:         []models.LabeledPrice{{Label: "Unlimited (1 month)", Amount: priceStars}},
		StartParameter: "unlimited_month",
		ProviderToken:  "",
	})
	return err == nil
}

func (bh *Handlers) sendSubscriptionInvoiceYooKassa(ctx context.Context, b *bot.Bot, chatID int64, lang i18n.Lang) bool {
	token := strings.TrimSpace(os.Getenv("YOOKASSA_PROVIDER_TOKEN"))
	if token == "" {
		return false
	}
	priceKopeks := getEnvInt("SUB_PRICE_RUB_KOPEKS", 15000)
	payload := strings.TrimSpace(os.Getenv("SUB_PAYLOAD"))
	if payload == "" {
		payload = "sub_unlimited_month"
	}
	_, err := b.SendInvoice(ctx, &bot.SendInvoiceParams{
		ChatID:         chatID,
		Title:          "Unlimited subscription",
		Description:    "Unlimited conversions for 1 month",
		Payload:        payload,
		ProviderToken:  token,
		Currency:       "RUB",
		Prices:         []models.LabeledPrice{{Label: "Unlimited (1 month)", Amount: priceKopeks}},
		StartParameter: "unlimited_month",
	})
	return err == nil
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
	lang := langFromSessionOrCtx(ctx, session)
	chatID := int64(0)
	messageID := 0
	if update.CallbackQuery.Message.Message != nil {
		chatID = update.CallbackQuery.Message.Message.Chat.ID
		messageID = update.CallbackQuery.Message.Message.ID
	}

	data, _ := contextkeys.GetCallbackData(ctx)
	if data == "" {
		data = update.CallbackQuery.Data
	}

	format, taskID, err := bh.parseClickButtonData(data)
	if err != nil {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}

	format = strings.ToLower(format)

	if format == "batch_sep" || format == "batch_all" {
		bh.handleBatchChoice(ctx, b, update, session, lang, taskID, format)
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

	if task.SessionID != session.ID {
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotInSession(lang))
		return
	}

	if task.Options == nil {
		task.Options = map[string]interface{}{}
	}

	if v, ok := task.Options["batch_mode"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "all" {
			bh.handleBatchFormatSelection(ctx, b, update, session, lang, task, targetExt)
			return
		}
	}
	fileSize := int64(0)
	if v, ok := task.Options["file_size"]; ok {
		switch t := v.(type) {
		case int64:
			fileSize = t
		case int:
			fileSize = int64(t)
		case float64:
			fileSize = int64(t)
		}
	}
	credits := 0
	heavy := false
	if v, ok := task.Options["pricing"]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			if cv, ok := m[strings.ToUpper(targetExt)]; ok {
				switch ct := cv.(type) {
				case int:
					credits = ct
				case int64:
					credits = int(ct)
				case float64:
					credits = int(ct)
				}
			}
		}
	}
	if credits == 0 {
		credits, heavy = pricing.Credits(task.OriginalExt, targetExt, fileSize)
	} else {
		_, heavy = pricing.Credits(task.OriginalExt, targetExt, fileSize)
	}
	unlimited := false
	remaining := 0
	if credits > 0 && bh.billing != nil {
		r, u, err := bh.billing.Consume(session.UserID, credits)
		if err != nil {
			if err == store.ErrInsufficientCredits {
				bh.refreshPendingSelections(ctx, b, session, lang, false, r, messageID, taskID)
				bh.refreshSelectionMessage(ctx, b, chatID, messageID, lang, task, false, r)
				_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInsufficientCredits(lang, r))
				return
			}
			log.Printf("Billing error: %v", err)
			_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackBillingError(lang))
			return
		}
		unlimited = u
		remaining = r
	} else if bh.billing != nil {
		u, _ := bh.billing.IsUnlimited(session.UserID)
		unlimited = u
		if !unlimited {
			r, _ := bh.billing.GetOrResetBalance(session.UserID)
			remaining = r
		}
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
	task.Options["credits"] = credits
	task.Options["is_heavy"] = heavy
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
	if !unlimited && bh.billing != nil {
		task.Options["credits_remaining"] = remaining
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
	if credits > 0 {
		statusText = statusText + "\n\n" + messages.TaskTypeLine(lang, heavy) + "\n" + messages.CreditsCostLine(lang, credits)
	}
	if bh.billing != nil {
		if unlimited {
			statusText = statusText + "\n\n" + messages.PlanUnlimitedLine(lang)
		} else {
			statusText = statusText + "\n\n" + messages.CreditsRemainingLine(lang, remaining)
		}
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

	bh.removePendingSelection(session, messageID, taskID)
	_ = bh.store.UpdateSession(session)
	bh.refreshPendingSelections(ctx, b, session, lang, unlimited, remaining, messageID, taskID)
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

func (bh *Handlers) filterButtonsByBalance(buttons []formats.FormatButton, unlimited bool, remaining int) []formats.FormatButton {
	if unlimited {
		return buttons
	}
	if remaining <= 0 {
		out := make([]formats.FormatButton, 0, len(buttons))
		for _, btn := range buttons {
			if btn.Credits == 0 {
				out = append(out, btn)
			}
		}
		return out
	}
	out := make([]formats.FormatButton, 0, len(buttons))
	for _, btn := range buttons {
		if btn.Credits == 0 || btn.Credits <= remaining {
			out = append(out, btn)
		}
	}
	return out
}

func (bh *Handlers) refreshSelectionMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int, lang i18n.Lang, task *types.Task, unlimited bool, remaining int) {
	if chatID == 0 || messageID == 0 || task == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	fileSize := int64(0)
	if task.Options != nil {
		if v, ok := task.Options["file_size"]; ok {
			switch t := v.(type) {
			case int64:
				fileSize = t
			case int:
				fileSize = int64(t)
			case float64:
				fileSize = int64(t)
			}
		}
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
		buttons = formats.GetButtonsForSourceExtWithCredits(task.OriginalExt, task.ID, fileSize, lang)
	}
	buttons = bh.filterButtonsByBalance(buttons, unlimited, remaining)
	text := ""
	if textInput {
		text = messages.TextReceivedChooseFormat(lang)
	} else {
		text = messages.FileReceivedChooseFormat(lang, task.FileName)
	}
	if bh.billing != nil {
		if unlimited {
			text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
		} else {
			text = text + "\n\n" + messages.CreditsRemainingLine(lang, remaining)
			if remaining <= 0 {
				text = text + "\n" + messages.NoCreditsHint(lang)
			}
		}
	}
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
		ParseMode: messages.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: bh.buildInlineKeyboard(buttons).InlineKeyboard,
		},
	})
}

func (bh *Handlers) refreshPendingSelections(ctx context.Context, b *bot.Bot, session *types.Session, lang i18n.Lang, unlimited bool, remaining int, excludeMessageID int, excludeTaskID string) {
	if session == nil {
		return
	}
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	next := make([]types.PendingSelection, 0, len(session.Pending))
	for _, p := range session.Pending {
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
		fileSize := int64(0)
		if task.Options != nil {
			if v, ok := task.Options["file_size"]; ok {
				switch t := v.(type) {
				case int64:
					fileSize = t
				case int:
					fileSize = int64(t)
				case float64:
					fileSize = int64(t)
				}
			}
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
			buttons = formats.GetButtonsForSourceExtWithCredits(task.OriginalExt, task.ID, fileSize, lang)
		}
		buttons = bh.filterButtonsByBalance(buttons, unlimited, remaining)
		text := ""
		if textInput {
			text = messages.TextReceivedChooseFormat(lang)
		} else {
			text = messages.FileReceivedChooseFormat(lang, task.FileName)
		}
		if bh.billing != nil {
			if unlimited {
				text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
			} else {
				text = text + "\n\n" + messages.CreditsRemainingLine(lang, remaining)
				if remaining <= 0 {
					text = text + "\n" + messages.NoCreditsHint(lang)
				}
			}
		}
		_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    session.ChatID,
			MessageID: p.MessageID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: bh.buildInlineKeyboard(buttons).InlineKeyboard,
			},
		})
		if err != nil {
			continue
		}
		next = append(next, p)
	}
	session.Pending = next
	_ = bh.store.UpdateSession(session)
}

func (bh *Handlers) removePendingSelection(session *types.Session, messageID int, taskID string) {
	if session == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if messageID == 0 && taskID == "" {
		return
	}
	next := make([]types.PendingSelection, 0, len(session.Pending))
	for _, p := range session.Pending {
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
	session.Pending = next
}

func (bh *Handlers) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	command := strings.TrimSpace(update.Message.Text)
	lang := langFromSessionOrCtx(ctx, session)
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return
	}
	cmd := fields[0]
	if strings.Contains(cmd, "@") {
		cmd = strings.SplitN(cmd, "@", 2)[0]
	}

	switch cmd {
	case "/grant_unlimited":
		if !isAdminUser(session.UserID) {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorUnknownCommand(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		secret := ""
		arg := ""
		if len(fields) >= 2 {
			secret = strings.TrimSpace(fields[1])
		}
		if len(fields) >= 3 {
			arg = strings.TrimSpace(fields[2])
		}
		if secret == "" {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.AdminGrantUsage(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		if !adminSecretOK(secret) {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.AdminDenied(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		if bh.userStore == nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorDefault(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}

		if arg == "" {
			arg = "30"
		}
		if strings.EqualFold(arg, "forever") {
			sub := types.Subscription{UserID: session.UserID, Plan: "unlimited", Status: "active", ExpiresAt: nil}
			_ = bh.userStore.UpsertSubscription(sub)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.AdminGrantDone(lang, nil),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		days, err := strconv.Atoi(arg)
		if err != nil || days <= 0 || days > 3650 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.AdminGrantUsage(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		sub, err := bh.userStore.ActivateOrExtendUnlimited(session.UserID, time.Duration(days)*24*time.Hour)
		if err != nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorDefault(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.AdminGrantDone(lang, sub.ExpiresAt),
			ParseMode: messages.ParseModeHTML,
		})
		return
	case "/start":
		session.State = types.StateStart
		session.TargetExt = ""
		if err := bh.store.UpdateSession(session); err != nil {
			log.Printf("Error updating session: %v", err)
		}
		bh.sendMainMenu(ctx, b, update.Message.Chat.ID, lang)
	case "/lang":
		if session.Options == nil {
			session.Options = map[string]interface{}{}
		}
		if len(fields) < 2 {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangUsage(lang),
				ParseMode: messages.ParseModeHTML,
			})
			return
		}
		arg := strings.ToLower(strings.TrimSpace(fields[1]))
		switch arg {
		case "ru", "en":
			session.Options["lang"] = arg
			_ = bh.store.UpdateSession(session)
			newLang := i18n.Parse(arg)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangSet(newLang),
				ParseMode: messages.ParseModeHTML,
			})
		case "auto":
			delete(session.Options, "lang")
			_ = bh.store.UpdateSession(session)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangAuto(langFromSessionOrCtx(ctx, session)),
				ParseMode: messages.ParseModeHTML,
			})
		default:
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangInvalid(lang),
				ParseMode: messages.ParseModeHTML,
			})
		}
	case "/balance":
		text := ""
		if bh.billing == nil {
			text = messages.BalanceUnavailable(lang)
		} else {
			unlimited, _ := bh.billing.IsUnlimited(session.UserID)
			if unlimited {
				text = messages.PlanUnlimitedLine(lang)
			} else {
				rem, err := bh.billing.GetOrResetBalance(session.UserID)
				if err != nil {
					text = messages.BalanceUnavailable(lang)
				} else {
					text = messages.CreditsRemainingLine(lang, rem)
				}
			}
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
		})
	case "/subscribe":
		unlimited := false
		if bh.billing != nil {
			u, _ := bh.billing.IsUnlimited(session.UserID)
			unlimited = u
		}
		text := messages.SubscriptionInfo(lang, unlimited) + "\n\n" + messages.MenuBtnContact(lang) + ": @esteticcus"
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
		})
	case "/help":
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      formats.GetHelpMessage(lang),
			ParseMode: messages.ParseModeHTML,
		})
	case "/menu":
		bh.sendMainMenu(ctx, b, update.Message.Chat.ID, lang)
	default:
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.ErrorUnknownCommand(lang),
			ParseMode: messages.ParseModeHTML,
		})
	}
}

func isAdminUser(userID int64) bool {
	raw := strings.TrimSpace(os.Getenv("ADMIN_USER_IDS"))
	if raw == "" {
		return false
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		if id == userID {
			return true
		}
	}
	return false
}

func adminSecretOK(secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	expected := strings.TrimSpace(os.Getenv("ADMIN_SECRET"))
	if expected == "" {
		return false
	}
	return secret == expected
}

func (bh *Handlers) handleBatchChoice(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session, lang i18n.Lang, batchTaskID string, choice string) {
	task, err := bh.store.GetTask(batchTaskID)
	if err != nil || task == nil || task.Options == nil {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotFound(lang))
		return
	}
	if task.SessionID != session.ID {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskNotInSession(lang))
		return
	}

	files := parseBatchFiles(task.Options["batch_files"])
	if len(files) < 2 {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}

	if choice == "batch_sep" {
		if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
			msg := update.CallbackQuery.Message.Message
			_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
		}
		_ = bh.store.DeleteTask(task.ID)
		for _, f := range files {
			bh.createAndAskFormatForSingleFile(ctx, b, session, lang, f)
		}
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		return
	}

	if choice == "batch_all" {
		task.Options["batch_mode"] = "all"
		_ = bh.store.UpdateTask(task)
		buttons := formats.GetBatchButtonsBySourceExtWithCredits(task.OriginalExt, task.ID, files, lang)
		if len(buttons) == 0 {
			_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.ErrorNoConversionOptions(lang, ""))
			return
		}
		keyboard := bh.buildInlineKeyboard(buttons)
		text := messages.BatchChooseFormat(lang, task.OriginalExt, len(files))
		if bh.billing != nil {
			unlimited, err := bh.billing.IsUnlimited(session.UserID)
			if err == nil && unlimited {
				text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
			} else if err == nil {
				rem, err := bh.billing.GetOrResetBalance(session.UserID)
				if err == nil {
					text = text + "\n\n" + messages.CreditsRemainingLine(lang, rem)
					if rem <= 0 {
						text = text + "\n" + messages.NoCreditsHint(lang)
					}
				}
			}
		}

		if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
			msg := update.CallbackQuery.Message.Message
			_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
		}
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    session.ChatID,
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

type batchFileParsed struct {
	FileID   string
	FileName string
	FileSize int64
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

func (bh *Handlers) createAndAskFormatForSingleFile(ctx context.Context, b *bot.Bot, session *types.Session, lang i18n.Lang, f formats.BatchFile) {
	fileName := strings.TrimSpace(f.FileName)
	if fileName == "" {
		fileName = fmt.Sprintf("file_%d.%s", time.Now().UnixNano(), strings.ToLower(strings.TrimSpace(session.TargetExt)))
	}
	ext := bh.getExtensionFromFileName(fileName)
	task, err := bh.store.SetProcessingFile(session.ID, f.FileID, fileName, f.FileSize)
	if err != nil {
		return
	}
	targets := formats.GetTargetFormatsForSourceExt(ext)
	priceMap := map[string]interface{}{}
	for _, t := range targets {
		credits, _ := pricing.Credits(ext, t, f.FileSize)
		if credits > 0 {
			priceMap[strings.ToUpper(t)] = credits
		}
	}
	if task.Options == nil {
		task.Options = map[string]interface{}{}
	}
	task.Options["pricing"] = priceMap
	task.Options["lang"] = string(lang)
	_ = bh.store.UpdateTask(task)
	buttons := formats.GetButtonsForSourceExtWithCredits(ext, task.ID, f.FileSize, lang)
	if len(buttons) == 0 {
		return
	}
	keyboard := bh.buildInlineKeyboard(buttons)
	text := messages.FileReceivedChooseFormat(lang, fileName)
	if bh.billing != nil {
		unlimited, err := bh.billing.IsUnlimited(session.UserID)
		if err == nil && unlimited {
			text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
		} else if err == nil {
			rem, err := bh.billing.GetOrResetBalance(session.UserID)
			if err == nil {
				text = text + "\n\n" + messages.CreditsRemainingLine(lang, rem)
				if rem <= 0 {
					text = text + "\n" + messages.NoCreditsHint(lang)
				}
			}
		}
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      session.ChatID,
		Text:        text,
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
}

func (bh *Handlers) handleBatchFormatSelection(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session, lang i18n.Lang, batchTask *types.Task, targetExt string) {
	files := parseBatchFiles(batchTask.Options["batch_files"])
	if len(files) < 2 {
		_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInvalidButtonData(lang))
		return
	}
	totalCredits := 0
	heavyAny := false
	for _, f := range files {
		c, h := pricing.Credits(batchTask.OriginalExt, targetExt, f.FileSize)
		totalCredits += c
		if h {
			heavyAny = true
		}
	}
	unlimited := false
	remaining := 0
	if totalCredits > 0 && bh.billing != nil {
		r, u, err := bh.billing.Consume(session.UserID, totalCredits)
		if err != nil {
			if err == store.ErrInsufficientCredits {
				_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackInsufficientCredits(lang, r))
				return
			}
			_ = bh.answerCallbackAlert(ctx, b, update.CallbackQuery.ID, messages.CallbackBillingError(lang))
			return
		}
		unlimited = u
		remaining = r
	}

	for _, f := range files {
		task := &types.Task{
			SessionID:   session.ID,
			State:       types.StateProcessing,
			FileID:      f.FileID,
			FileName:    f.FileName,
			OriginalExt: batchTask.OriginalExt,
			TargetExt:   targetExt,
			Options: map[string]interface{}{
				"file_size":           f.FileSize,
				"lang":                string(lang),
				"unlimited":           unlimited,
				"priority":            unlimited,
				"credits":             0,
				"is_heavy":            heavyAny,
				"credits_remaining":   remaining,
				"batch_parent_task":   batchTask.ID,
				"batch_total_credits": totalCredits,
			},
		}
		_ = bh.store.CreateTask(task)
		bh.scheduler.EnqueueTask(task.ID, 0, 0, task.FileName, lang, unlimited)
	}

	_ = bh.store.DeleteTask(batchTask.ID)

	if update != nil && update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
		msg := update.CallbackQuery.Message.Message
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
	}
	text := messages.BatchStarted(lang, len(files))
	if totalCredits > 0 {
		text = text + "\n\n" + messages.TaskTypeLine(lang, heavyAny) + "\n" + messages.CreditsCostLine(lang, totalCredits)
	}
	if bh.billing != nil {
		if unlimited {
			text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
		} else {
			text = text + "\n\n" + messages.CreditsRemainingLine(lang, remaining)
			if remaining <= 0 {
				text = text + "\n" + messages.NoCreditsHint(lang)
			}
		}
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    session.ChatID,
		Text:      text,
		ParseMode: messages.ParseModeHTML,
	})
	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
}

func (bh *Handlers) HandleFile(ctx context.Context, b *bot.Bot, update *models.Update, session *types.Session) {
	filesInfo, hasFiles := contextkeys.GetFilesInfo(ctx)
	lang := langFromSessionOrCtx(ctx, session)
	if !hasFiles || filesInfo == nil || len(filesInfo.Files) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      messages.ErrorCannotProcessFile(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	if session.Options == nil {
		session.Options = map[string]interface{}{}
	}

	mediaGroupID := ""
	if update != nil && update.Message != nil {
		mediaGroupID = strings.TrimSpace(update.Message.MediaGroupID)
	}
	collectKey := session.ID
	if mediaGroupID != "" {
		collectKey = session.ID + ":mg:" + mediaGroupID
	}
	bh.appendToUserBatch(ctx, b, collectKey, update.Message.Chat.ID, session, lang, filesInfo.Files)
}

func (bh *Handlers) appendToUserBatch(ctx context.Context, b *bot.Bot, collectKey string, chatID int64, session *types.Session, lang i18n.Lang, files []contextkeys.FileInfo) {
	if session == nil || len(files) == 0 {
		return
	}
	if session.Options == nil {
		session.Options = map[string]interface{}{}
	}

	collectKey = strings.TrimSpace(collectKey)
	if collectKey == "" {
		collectKey = session.ID
	}

	taskID := ""
	bh.batchMu.Lock()
	if v, ok := bh.batchTaskID[collectKey]; ok {
		taskID = strings.TrimSpace(v)
	}
	bh.batchMu.Unlock()
	if taskID == "" && collectKey == session.ID {
		if v, ok := session.Options["collect_task_id"]; ok {
			if s, ok := v.(string); ok {
				taskID = strings.TrimSpace(s)
			}
		}
	}

	var task *types.Task
	if taskID != "" {
		t, err := bh.store.GetTask(taskID)
		if err == nil && t != nil && t.SessionID == session.ID && t.Options != nil {
			task = t
		} else {
			taskID = ""
		}
	}

	if task == nil {
		tasks, err := bh.store.GetSessionTasks(session.ID)
		if err == nil {
			for i := len(tasks) - 1; i >= 0; i-- {
				t := tasks[i]
				if t == nil || t.Options == nil {
					continue
				}
				if v, ok := t.Options["collect_key"]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" && strings.TrimSpace(s) != collectKey {
						continue
					}
				}
				if v, ok := t.Options["collecting"]; ok {
					if bv, ok := v.(bool); ok && bv && t.State == types.StateChooseExt {
						task = t
						taskID = t.ID
						if collectKey == session.ID {
							session.Options["collect_task_id"] = taskID
							_ = bh.store.UpdateSession(session)
						}
						break
					}
				}
			}
		}
	}

	if task == nil {
		task = &types.Task{
			SessionID:   session.ID,
			State:       types.StateChooseExt,
			FileID:      "",
			FileName:    "batch",
			OriginalExt: "",
			TargetExt:   "",
			Options: map[string]interface{}{
				"lang":        string(lang),
				"collecting":  true,
				"collect_key": collectKey,
				"batch_files": []interface{}{},
			},
		}
		_ = bh.store.CreateTask(task)
		taskID = task.ID
		if collectKey == session.ID {
			session.Options["collect_task_id"] = taskID
			_ = bh.store.UpdateSession(session)
		}
	}

	list := []interface{}{}
	if v, ok := task.Options["batch_files"]; ok {
		if arr, ok := v.([]interface{}); ok {
			list = arr
		}
	}
	now := time.Now().UnixNano()
	for _, fi := range files {
		fileName := strings.TrimSpace(fi.FileName)
		if strings.EqualFold(fileName, "photo.jpg") {
			fileName = fmt.Sprintf("photo_%d.jpg", time.Now().UnixNano())
		}
		list = append(list, map[string]interface{}{
			"file_id":   fi.FileID,
			"file_name": fileName,
			"file_size": fi.FileSize,
		})
	}
	task.Options["batch_files"] = list
	task.Options["collect_last"] = now
	task.Options["lang"] = string(lang)
	_ = bh.store.UpdateTask(task)

	bh.resetBatchTimer(b, collectKey, session.ID, task.ID)
}

func (bh *Handlers) resetBatchTimer(b *bot.Bot, collectKey string, sessionID string, collectorTaskID string) {
	window := 900 * time.Millisecond
	if strings.Contains(collectKey, ":mg:") {
		window = 1500 * time.Millisecond
	}
	bh.batchMu.Lock()
	if t, ok := bh.batchTimers[collectKey]; ok && t != nil {
		t.Stop()
	}
	bh.batchTaskID[collectKey] = collectorTaskID
	bh.batchTimers[collectKey] = time.AfterFunc(window, func() {
		bh.batchMu.Lock()
		current := bh.batchTaskID[collectKey]
		bh.batchMu.Unlock()
		if strings.TrimSpace(current) != strings.TrimSpace(collectorTaskID) {
			return
		}
		bh.finalizeUserBatch(b, collectKey, sessionID, collectorTaskID)
	})
	bh.batchMu.Unlock()
}

func (bh *Handlers) finalizeUserBatch(b *bot.Bot, collectKey string, sessionID string, collectorTaskID string) {
	task, err := bh.store.GetTask(collectorTaskID)
	if err != nil || task == nil || task.Options == nil {
		return
	}
	session, err := bh.store.GetSession(sessionID)
	if err != nil || session == nil {
		return
	}

	lang := i18n.EN
	if v, ok := task.Options["lang"]; ok {
		if s, ok := v.(string); ok {
			lang = i18n.Parse(s)
		}
	}

	chatID := session.ChatID

	files := parseBatchFiles(task.Options["batch_files"])
	if len(files) == 0 {
		_ = bh.store.DeleteTask(task.ID)
		delete(session.Options, "collect_task_id")
		_ = bh.store.UpdateSession(session)
		return
	}

	type g struct {
		ext   string
		files []formats.BatchFile
	}
	byExt := map[string][]formats.BatchFile{}
	order := make([]string, 0)
	for _, f := range files {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(bh.getExtensionFromFileName(f.FileName)), "."))
		if ext == "" {
			ext = "_unknown_"
		}
		if _, ok := byExt[ext]; !ok {
			order = append(order, ext)
		}
		byExt[ext] = append(byExt[ext], f)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if collectKey == session.ID {
		delete(session.Options, "collect_task_id")
		_ = bh.store.UpdateSession(session)
	}
	_ = bh.store.DeleteTask(task.ID)

	bh.batchMu.Lock()
	if t, ok := bh.batchTimers[collectKey]; ok && t != nil {
		t.Stop()
	}
	delete(bh.batchTimers, collectKey)
	delete(bh.batchTaskID, collectKey)
	bh.batchMu.Unlock()

	unlimited := false
	remaining := 0
	if bh.billing != nil {
		u, err := bh.billing.IsUnlimited(session.UserID)
		if err == nil {
			unlimited = u
		}
		if !unlimited {
			r, err := bh.billing.GetOrResetBalance(session.UserID)
			if err == nil {
				remaining = r
			}
		}
	}

	for _, extKey := range order {
		groupFiles := byExt[extKey]
		if len(groupFiles) > 1 && extKey != "_unknown_" {
			targets := formats.GetTargetFormatsForSourceExt(extKey)
			if len(targets) > 0 {
				bt := &types.Task{
					SessionID:   session.ID,
					State:       types.StateChooseExt,
					FileID:      groupFiles[0].FileID,
					FileName:    fmt.Sprintf("%d files.%s", len(groupFiles), extKey),
					OriginalExt: extKey,
					TargetExt:   "",
					Options: map[string]interface{}{
						"lang":       string(lang),
						"batch_mode": "",
					},
				}
				batchFiles := make([]map[string]interface{}, 0, len(groupFiles))
				for _, gf := range groupFiles {
					batchFiles = append(batchFiles, map[string]interface{}{
						"file_id":   gf.FileID,
						"file_name": gf.FileName,
						"file_size": gf.FileSize,
					})
				}
				bt.Options["batch_files"] = batchFiles
				_ = bh.store.CreateTask(bt)

				rows := [][]models.InlineKeyboardButton{
					{
						{Text: " " + messages.BatchBtnAll(lang) + " ", CallbackData: "batch_all_for_" + bt.ID},
					},
					{
						{Text: " " + messages.BatchBtnSeparate(lang) + " ", CallbackData: "batch_sep_for_" + bt.ID},
					},
				}
				text := messages.BatchReceivedChoice(lang, extKey, len(groupFiles))
				if bh.billing != nil {
					if unlimited {
						text = text + "\n\n" + messages.PlanUnlimitedLine(lang)
					} else {
						text = text + "\n\n" + messages.CreditsRemainingLine(lang, remaining)
						if remaining <= 0 {
							text = text + "\n" + messages.NoCreditsHint(lang)
						}
					}
				}
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:    chatID,
					Text:      text,
					ParseMode: messages.ParseModeHTML,
					ReplyMarkup: &models.InlineKeyboardMarkup{
						InlineKeyboard: rows,
					},
				})
				continue
			}
		}

		for _, f := range groupFiles {
			bh.createAndAskFormatForSingleFile(ctx, b, session, lang, f)
		}
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
	pad := func(s string) string { return " " + s + " " }
	rows := make([][]models.InlineKeyboardButton, 0)
	row := make([]models.InlineKeyboardButton, 0, 3)
	for i, button := range buttons {
		if i > 0 && i%3 == 0 {
			rows = append(rows, row)
			row = make([]models.InlineKeyboardButton, 0, 3)
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         pad(button.Text),
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
	lang := langFromSessionOrCtx(ctx, session)
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
	task, err := bh.store.SetProcessingFile(session.ID, fileID, fileName, fileSize)
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
	keyboard := bh.buildInlineKeyboard(buttons)
	textOut := messages.TextReceivedChooseFormat(lang)
	if bh.billing != nil {
		unlimited, err := bh.billing.IsUnlimited(session.UserID)
		if err == nil && unlimited {
			textOut = textOut + "\n\n" + messages.PlanUnlimitedLine(lang)
		} else if err == nil {
			rem, err := bh.billing.GetOrResetBalance(session.UserID)
			if err == nil {
				textOut = textOut + "\n\n" + messages.CreditsRemainingLine(lang, rem)
				if rem <= 0 {
					textOut = textOut + "\n" + messages.NoCreditsHint(lang)
				}
			}
		}
	}
	sent, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        textOut,
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
	if sent != nil {
		bh.addPendingSelection(session, sent.ID, task.ID)
		_ = bh.store.UpdateSession(session)
	}
}

func (bh *Handlers) addPendingSelection(session *types.Session, messageID int, taskID string) {
	if session == nil || messageID == 0 || strings.TrimSpace(taskID) == "" {
		return
	}
	next := make([]types.PendingSelection, 0, len(session.Pending)+1)
	for _, p := range session.Pending {
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
	session.Pending = next
}
