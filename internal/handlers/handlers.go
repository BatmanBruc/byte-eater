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
		store:     store,
		scheduler: scheduler,
		userStore: userStore,
		billing:   billing,
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

	_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")

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
	if !formats.FormatExists(format) {
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
			if cv, ok := m[strings.ToUpper(format)]; ok {
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
		credits, heavy = pricing.Credits(task.OriginalExt, format, fileSize)
	} else {
		_, heavy = pricing.Credits(task.OriginalExt, format, fileSize)
	}
	unlimited := false
	remaining := 0
	if credits > 0 && bh.billing != nil {
		r, u, err := bh.billing.Consume(session.UserID, credits)
		if err != nil {
			if err == store.ErrInsufficientCredits {
				_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackInsufficientCredits(lang, r))
				return
			}
			log.Printf("Billing error: %v", err)
			_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackBillingError(lang))
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

	task.TargetExt = format
	task.State = types.StateProcessing
	task.Options["credits"] = credits
	task.Options["is_heavy"] = heavy
	task.Options["unlimited"] = unlimited
	task.Options["lang"] = string(lang)
	if !unlimited && bh.billing != nil {
		task.Options["credits_remaining"] = remaining
	}
	if err := bh.store.UpdateTask(task); err != nil {
		log.Printf("Error updating task %s: %v", taskID, err)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, messages.CallbackTaskUpdateFailed(lang))
		return
	}

	chatID := int64(0)
	messageID := 0
	if update.CallbackQuery.Message.Message != nil {
		chatID = update.CallbackQuery.Message.Message.Chat.ID
		messageID = update.CallbackQuery.Message.Message.ID
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

	for _, fileInfo := range filesInfo.Files {
		fileName := strings.TrimSpace(fileInfo.FileName)
		if strings.EqualFold(fileName, "photo.jpg") {
			fileName = fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
		}
		ext := bh.getExtensionFromFileName(fileName)
		category := formats.GetCategoryByExtension(ext)
		if category == "" {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorCannotDetectFileType(lang, fileName),
				ParseMode: messages.ParseModeHTML,
			})
			continue
		}

		task, err := bh.store.SetProcessingFile(session.ID, fileInfo.FileID, fileName, fileInfo.FileSize)
		if err != nil {
			log.Printf("Error setting processing file: %v", err)
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorDefault(lang),
				ParseMode: messages.ParseModeHTML,
			})
			continue
		}

		targets := formats.GetTargetFormatsForSourceExt(ext)
		priceMap := map[string]interface{}{}
		for _, t := range targets {
			credits, _ := pricing.Credits(ext, t, fileInfo.FileSize)
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

		buttons := formats.GetFormatButtonsBySourceExtWithCredits(ext, task.ID, fileInfo.FileSize)
		if len(buttons) == 0 {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.ErrorNoConversionOptions(lang, fileName),
				ParseMode: messages.ParseModeHTML,
			})
			continue
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
				}
			}
		}

		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      update.Message.Chat.ID,
			Text:        text,
			ParseMode:   messages.ParseModeHTML,
			ReplyMarkup: keyboard,
		})
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
			}
		}
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        textOut,
		ParseMode:   messages.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
}
