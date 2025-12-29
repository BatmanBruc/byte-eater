package messages

import (
	"fmt"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
)

const ParseModeHTML = "HTML"

func Escape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(strings.TrimSpace(s))
}

func Title(text string) string {
	return fmt.Sprintf("✨ <b>%s</b>", Escape(text))
}

func pick(lang i18n.Lang, ru string, en string) string {
	if lang == i18n.RU {
		return ru
	}
	return en
}

func FileLine(lang i18n.Lang, fileName string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = pick(lang, "файл", "file")
	}
	return fmt.Sprintf("📄 <b>%s</b> %s", Escape(pick(lang, "Файл:", "File:")), Escape(name))
}

func ErrorDefault(lang i18n.Lang) string {
	return pick(lang, "🚫 <b>Ошибка</b>\nПопробуйте ещё раз.", "🚫 <b>Error</b>\nPlease try again.")
}

func ErrorUnsupportedMessageType(lang i18n.Lang) string {
	return pick(lang, "🤖 <b>Я так не умею</b>\nОтправьте файл или текст.", "🤖 <b>I can't handle that</b>\nSend a file or text.")
}

func ErrorCannotProcessFile(lang i18n.Lang) string {
	return pick(lang, "🚫 <b>Не удалось обработать файл</b>\nПопробуйте отправить снова.", "🚫 <b>Couldn't process the file</b>\nPlease send it again.")
}

func ErrorUnknownCommand(lang i18n.Lang) string {
	return pick(lang, "❓ <b>Команда не найдена</b>", "❓ <b>Unknown command</b>")
}

func StartWelcome(lang i18n.Lang) string {
	if lang == i18n.RU {
		return "👋 <b>Привет!</b>\nЯ конвертирую файлы.\n\n" +
			"📎 Отправьте файл (документ/фото/видео/аудио), <b>войс</b> или <b>кружок</b>, либо просто текст.\n" +
			"🧩 Выберите формат в появившихся кнопках."
	}
	return "👋 <b>Hi!</b>\nI convert files.\n\n" +
		"📎 Send a file (document/photo/video/audio), a <b>voice message</b>, a <b>video note</b>, or just text.\n" +
		"🧩 Pick the target format from the buttons."
}

func HelpHeader(lang i18n.Lang) string {
	return pick(lang, "ℹ️ <b>Поддерживаемые форматы</b>\n", "ℹ️ <b>Supported formats</b>\n")
}

func QueueAlreadyQueued(lang i18n.Lang, fileName string) string {
	return pick(lang, "⚠️ <b>Уже в очереди</b>\n", "⚠️ <b>Already queued</b>\n") + FileLine(lang, fileName)
}

func QueueQueued(lang i18n.Lang, fileName string, position int) string {
	return fmt.Sprintf("⏳ <b>%s</b> %d\n%s", Escape(pick(lang, "В очереди:", "In queue:")), position, FileLine(lang, fileName))
}

func QueueStarted(lang i18n.Lang, fileName string) string {
	return pick(lang, "⚙️ <b>Конвертация началась</b>\n", "⚙️ <b>Conversion started</b>\n") + FileLine(lang, fileName)
}

func TextReceivedChooseFormat(lang i18n.Lang) string {
	return pick(lang, "📝 <b>Текст получен</b>\nВыберите формат файла:", "📝 <b>Text received</b>\nChoose the output format:")
}

func FileReceivedChooseFormat(lang i18n.Lang, fileName string) string {
	return pick(lang, "📥 <b>Файл получен</b>\n", "📥 <b>File received</b>\n") + FileLine(lang, fileName) + pick(lang, "\n\nВыберите формат для конвертации:", "\n\nChoose the target format:")
}

func ErrorCannotDetectFileType(lang i18n.Lang, fileName string) string {
	return pick(lang, "🚫 <b>Не удалось определить тип файла</b>\n", "🚫 <b>Couldn't detect file type</b>\n") + FileLine(lang, fileName)
}

func ErrorCannotGetFormats(lang i18n.Lang) string {
	return pick(lang, "🚫 <b>Не удалось получить список форматов</b>\nПопробуйте ещё раз.", "🚫 <b>Couldn't get formats</b>\nPlease try again.")
}

func ErrorNoConversionOptions(lang i18n.Lang, fileName string) string {
	return pick(lang, "🚫 <b>Конвертация для этого формата пока не поддерживается</b>\n", "🚫 <b>This file type is not supported yet</b>\n") + FileLine(lang, fileName)
}

func EmptyTextHint(lang i18n.Lang) string {
	return pick(lang, "✍️ <b>Пустой текст</b>\nОтправьте текст, и я превращу его в файл.", "✍️ <b>Empty text</b>\nSend some text and I will turn it into a file.")
}

func ErrorUploadTextAsFile(lang i18n.Lang) string {
	return pick(lang, "🚫 <b>Не удалось загрузить текст как файл</b>\nПопробуйте ещё раз.", "🚫 <b>Couldn't upload text as a file</b>\nPlease try again.")
}

func ErrorConversionFailed(lang i18n.Lang, fileName string, err error) string {
	msg := pick(lang, "🚫 <b>Ошибка конвертации</b>\n", "🚫 <b>Conversion failed</b>\n") + FileLine(lang, fileName)
	if err != nil {
		msg += "\n\n" + fmt.Sprintf("<code>%s</code>", Escape(err.Error()))
	}
	return msg
}

func PlanUnlimitedLine(lang i18n.Lang) string {
	return pick(lang, "Тариф: безлимит", "Plan: unlimited")
}

func CreditsRemainingLine(lang i18n.Lang, remaining int) string {
	if lang == i18n.RU {
		return fmt.Sprintf("Осталось кредитов: %d/50", remaining)
	}
	return fmt.Sprintf("Remaining credits: %d/50", remaining)
}

func BalanceUnavailable(lang i18n.Lang) string {
	return pick(lang, "Баланс недоступен", "Balance is unavailable")
}

func CallbackInvalidButtonData(lang i18n.Lang) string {
	return pick(lang, "Некорректные данные кнопки", "Invalid button data")
}

func CallbackUnsupportedFormat(lang i18n.Lang) string {
	return pick(lang, "Неподдерживаемый формат", "Unsupported format")
}

func CallbackTaskNotFound(lang i18n.Lang) string {
	return pick(lang, "Задача не найдена", "Task not found")
}

func CallbackTaskNotInSession(lang i18n.Lang) string {
	return pick(lang, "Эта задача не принадлежит текущей сессии", "This task does not belong to the current session")
}

func CallbackTaskUpdateFailed(lang i18n.Lang) string {
	return pick(lang, "Не удалось обновить задачу", "Failed to update task")
}

func CallbackBillingError(lang i18n.Lang) string {
	return pick(lang, "Ошибка списания кредитов", "Failed to charge credits")
}

func CallbackInsufficientCredits(lang i18n.Lang, remaining int) string {
	if lang == i18n.RU {
		return fmt.Sprintf("Недостаточно кредитов. Осталось %d/50", remaining)
	}
	return fmt.Sprintf("Not enough credits. Remaining %d/50", remaining)
}

func TaskTypeLine(lang i18n.Lang, heavy bool) string {
	if lang == i18n.RU {
		if heavy {
			return "Тип: тяжелая"
		}
		return "Тип: легкая"
	}
	if heavy {
		return "Type: heavy"
	}
	return "Type: light"
}

func CreditsCostLine(lang i18n.Lang, credits int) string {
	if lang == i18n.RU {
		return fmt.Sprintf("Кредиты: %d", credits)
	}
	return fmt.Sprintf("Credits: %d", credits)
}

func LangUsage(lang i18n.Lang) string {
	return pick(lang,
		"🌐 <b>Язык</b>\nИспользование: <code>/lang ru</code> или <code>/lang en</code>\nЧтобы вернуться к автоопределению: <code>/lang auto</code>",
		"🌐 <b>Language</b>\nUsage: <code>/lang ru</code> or <code>/lang en</code>\nTo return to auto-detect: <code>/lang auto</code>",
	)
}

func LangSet(lang i18n.Lang) string {
	return pick(lang, "✅ Язык установлен", "✅ Language set")
}

func LangAuto(lang i18n.Lang) string {
	return pick(lang, "✅ Автоопределение языка включено", "✅ Language auto-detect enabled")
}

func LangInvalid(lang i18n.Lang) string {
	return pick(lang, "🚫 Неверный язык. Используйте: <code>/lang ru</code> или <code>/lang en</code>", "🚫 Invalid language. Use: <code>/lang ru</code> or <code>/lang en</code>")
}

func MenuTitle(lang i18n.Lang) string {
	return pick(lang, "📋 <b>Меню</b>", "📋 <b>Menu</b>")
}

func MainMenuText(lang i18n.Lang) string {
	if lang == i18n.RU {
		return StartWelcome(lang) + "\n\n" + "👇 <b>Меню</b>\nВыберите действие:"
	}
	return StartWelcome(lang) + "\n\n" + "👇 <b>Menu</b>\nChoose an option:"
}

func MenuBtnSubscription(lang i18n.Lang) string {
	return pick(lang, "💎 Подписка", "💎 Subscription")
}

func MenuBtnContact(lang i18n.Lang) string {
	return pick(lang, "👤 Контакт", "👤 Contact")
}

func MenuBtnAbout(lang i18n.Lang) string {
	return pick(lang, "ℹ️ О боте", "ℹ️ About")
}

func MenuBtnBack(lang i18n.Lang) string {
	return pick(lang, "⬅️ Назад", "⬅️ Back")
}

func MenuBtnSubscribeNow(lang i18n.Lang, active bool) string {
	if active {
		return pick(lang, "✅ Продлить", "✅ Renew")
	}
	return pick(lang, "✅ Оплатить", "✅ Pay")
}

func AboutCreditsBlock(lang i18n.Lang) string {
	return pick(lang,
		"💳 <b>Кредиты</b>\n- Без подписки: 50 кредитов в сутки (обновляются каждый день)\n- Подписка: кредиты не нужны (безлимит)\n\nКоманды: <code>/balance</code>, <code>/menu</code>",
		"💳 <b>Credits</b>\n- No subscription: 50 credits per day (refreshed daily)\n- Subscription: credits are not needed (unlimited)\n\nCommands: <code>/balance</code>, <code>/menu</code>",
	)
}

func SubscriptionInfo(lang i18n.Lang, unlimited bool) string {
	if unlimited {
		return pick(lang, "💎 <b>Подписка активна</b>\nТариф: безлимит", "💎 <b>Subscription active</b>\nPlan: unlimited")
	}
	return pick(lang, "💎 <b>Подписка не активна</b>\nЧтобы подключить безлимит — напишите @esteticcus", "💎 <b>Subscription inactive</b>\nTo enable unlimited, message @esteticcus")
}

func SubscriptionOffer(lang i18n.Lang) string {
	return pick(lang,
		"💎 <b>Подписка</b>\n\n"+
			"✅ <b>Безграничный лимит на конвертации</b>\n"+
			"— кредиты не списываются\n"+
			"— можно конвертировать сколько угодно\n\n"+
			"⚡ <b>Приоритетная очередь</b>\n"+
			"— задачи обрабатываются раньше обычных\n\n"+
			"Цена: <b>150 ₽/мес</b>\n\n"+
			"Чтобы подключить — нажмите кнопку ниже.",
		"💎 <b>Subscription</b>\n\n"+
			"✅ <b>Unlimited conversions</b>\n"+
			"— credits are not charged\n"+
			"— convert as much as you need\n\n"+
			"⚡ <b>Priority queue</b>\n"+
			"— your tasks are processed before regular ones\n\n"+
			"Price: <b>150 RUB / month</b>\n\n"+
			"To subscribe, press the button below.",
	)
}

func SubscriptionActiveDetails(lang i18n.Lang, expiresAt *time.Time) string {
	until := ""
	if expiresAt != nil {
		until = expiresAt.UTC().Format("2006-01-02")
	} else {
		if lang == i18n.RU {
			until = "бессрочно"
		} else {
			until = "forever"
		}
	}
	if lang == i18n.RU {
		return "💎 <b>Подписка активна</b>\n\n" +
			"Тариф: <b>Безлимит</b>\n" +
			"Активна до: <b>" + Escape(until) + "</b>\n\n" +
			"Что включено:\n" +
			"✅ безграничный лимит на конвертации (кредиты не списываются)\n" +
			"⚡ приоритетная очередь"
	}
	return "💎 <b>Subscription active</b>\n\n" +
		"Plan: <b>Unlimited</b>\n" +
		"Active until: <b>" + Escape(until) + "</b>\n\n" +
		"Included:\n" +
		"✅ unlimited conversions (credits are not charged)\n" +
		"⚡ priority queue"
}

func PayMethodTitle(lang i18n.Lang) string {
	return pick(lang, "💳 <b>Оплата</b>\nВыберите способ:", "💳 <b>Payment</b>\nChoose a method:")
}

func PayBtnStars(lang i18n.Lang) string {
	return pick(lang, "⭐ Оплатить Stars", "⭐ Pay with Stars")
}

func PayBtnYooKassa(lang i18n.Lang) string {
	return pick(lang, "💳 Оплатить ЮKassa", "💳 Pay with YooKassa")
}

func PaymentCreated(lang i18n.Lang) string {
	return pick(lang, "Счёт отправлен", "Invoice sent")
}

func PaymentNotConfigured(lang i18n.Lang) string {
	return pick(lang, "Оплата временно недоступна", "Payments are temporarily unavailable")
}

func PaymentSucceeded(lang i18n.Lang, until time.Time) string {
	if lang == i18n.RU {
		return fmt.Sprintf("✅ Оплата прошла успешно.\nПодписка активна до: <b>%s</b>", until.Format("2006-01-02"))
	}
	return fmt.Sprintf("✅ Payment successful.\nSubscription active until: <b>%s</b>", until.Format("2006-01-02"))
}

func PaymentAlreadyProcessed(lang i18n.Lang) string {
	return pick(lang, "✅ Платёж уже обработан", "✅ Payment already processed")
}
