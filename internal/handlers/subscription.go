package handlers

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) HandlePreCheckout(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	if update == nil || update.PreCheckoutQuery == nil {
		return
	}
	lang := bh.langFromUserOrCtx(ctx, userID)
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

func (bh *Handlers) HandleSuccessfulPayment(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	if update == nil || update.Message == nil || update.Message.SuccessfulPayment == nil {
		return
	}
	lang := bh.langFromUserOrCtx(ctx, userID)
	chatID := getChatIDFromUpdate(update)
	if chatID == 0 {
		chatID = userID
	}
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
		UserID: userID,
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
			ChatID:    chatID,
			Text:      messages.PaymentAlreadyProcessed(lang),
			ParseMode: messages.ParseModeHTML,
		})
		return
	}

	sub, err := bh.userStore.ActivateOrExtendUnlimited(userID, 30*24*time.Hour)
	if err != nil {
		return
	}
	until := time.Now().UTC()
	if sub != nil && sub.ExpiresAt != nil {
		until = sub.ExpiresAt.UTC()
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      messages.PaymentSucceeded(lang, until),
		ParseMode: messages.ParseModeHTML,
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
	_ = lang
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
	_ = lang
	return err == nil
}
