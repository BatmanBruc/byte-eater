package handlers

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (bh *Handlers) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update, userID int64) {
	command := strings.TrimSpace(update.Message.Text)
	lang := bh.langFromUserOrCtx(ctx, userID)
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
		if !isAdminUser(userID) {
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
			sub := types.Subscription{UserID: userID, Plan: "unlimited", Status: "active", ExpiresAt: nil}
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
		sub, err := bh.userStore.ActivateOrExtendUnlimited(userID, time.Duration(days)*24*time.Hour)
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
		bh.sendMainMenu(ctx, b, update.Message.Chat.ID, lang)
	case "/lang":
		options, _ := bh.userState.GetUserOptions(userID)
		if options == nil {
			options = map[string]interface{}{}
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
			options["lang"] = arg
			_ = bh.userState.SetUserOptions(userID, options)
			newLang := i18n.Parse(arg)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangSet(newLang),
				ParseMode: messages.ParseModeHTML,
			})
		case "auto":
			delete(options, "lang")
			_ = bh.userState.SetUserOptions(userID, options)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    update.Message.Chat.ID,
				Text:      messages.LangAuto(bh.langFromUserOrCtx(ctx, userID)),
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
		text := messages.BalanceUnavailable(lang)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      text,
			ParseMode: messages.ParseModeHTML,
		})
	case "/subscribe":
		unlimited := false
		if bh.billing != nil {
			u, _ := bh.billing.IsUnlimited(userID)
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
