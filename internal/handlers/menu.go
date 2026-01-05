package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/contextkeys"
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) buildMenuKeyboard(lang i18n.Lang, withBack bool) models.InlineKeyboardMarkup {
	pad := func(s string) string { return "   " + s + "   " }
	rows := make([][]models.InlineKeyboardButton, 0, 4)
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: pad(messages.MenuBtnBatch(lang)), CallbackData: "menu_batch"},
	})
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: pad(messages.MenuBtnMergePDF(lang)), CallbackData: "menu_merge_pdf"},
	})
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

func (bh *Handlers) HandleMenuClick(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	lang := bh.langFromUserOrCtx(ctx, userID)
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
	chatID := getChatIDFromUpdate(update)
	if chatID == 0 {
		chatID = userID
	}

	text := messages.MainMenuText(lang)
	keyboard := bh.buildMenuKeyboard(lang, false)

	switch data {
	case "menu_batch":
		options, _ := bh.userState.GetUserOptions(userID)
		if options == nil {
			options = map[string]interface{}{}
		}
		delete(options, "mb_state")
		delete(options, "mb_expected")
		delete(options, "mb_files")
		_ = bh.userState.SetUserOptions(userID, options)

		options["mb_state"] = "await_count"
		_ = bh.userState.SetUserOptions(userID, options)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.BatchHowManyPrompt(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	case "menu_merge_pdf":
		options, _ := bh.userState.GetUserOptions(userID)
		if options == nil {
			options = map[string]interface{}{}
		}
		delete(options, "merge_state")
		delete(options, "merge_files")
		delete(options, "merge_msg_id")
		options["merge_state"] = "waiting"
		_ = bh.userState.SetUserOptions(userID, options)
		_ = bh.answerCallback(ctx, b, update.CallbackQuery.ID, "")
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      messages.MergePDFWaiting(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	case "menu_sub":
		active := false
		var expiresAt *time.Time
		if bh.userStore != nil {
			sub, err := bh.userStore.GetSubscription(userID)
			if err == nil && sub != nil {
				if strings.EqualFold(strings.TrimSpace(sub.Status), "active") && strings.EqualFold(strings.TrimSpace(sub.Plan), "unlimited") {
					if sub.ExpiresAt == nil || sub.ExpiresAt.After(time.Now()) {
						active = true
						expiresAt = sub.ExpiresAt
					}
				}
			}
		} else if bh.billing != nil {
			u, _ := bh.billing.IsUnlimited(userID)
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
