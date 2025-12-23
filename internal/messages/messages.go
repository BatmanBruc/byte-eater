package messages

import (
	"fmt"
	"strings"
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

func FileLine(fileName string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "файл"
	}
	return fmt.Sprintf("📄 <b>Файл:</b> %s", Escape(name))
}

func ErrorDefault() string {
	return "🚫 <b>Ошибка</b>\nПопробуйте ещё раз."
}

func ErrorUnsupportedMessageType() string {
	return "🤖 <b>Я так не умею</b>\nОтправьте файл или текст."
}

func ErrorCannotProcessFile() string {
	return "🚫 <b>Не удалось обработать файл</b>\nПопробуйте отправить снова."
}

func ErrorUnknownCommand() string {
	return "❓ <b>Команда не найдена</b>"
}

func StartWelcome() string {
	return "👋 <b>Привет!</b>\nЯ конвертирую файлы.\n\n" +
		"📎 Отправьте файл (документ/фото/видео/аудио) или просто текст.\n" +
		"🧩 Выберите формат в появившихся кнопках."
}

func HelpHeader() string {
	return "ℹ️ <b>Поддерживаемые форматы</b>\n"
}

func QueueAlreadyQueued(fileName string) string {
	return "⚠️ <b>Уже в очереди</b>\n" + FileLine(fileName)
}

func QueueQueued(fileName string, position int) string {
	return fmt.Sprintf("⏳ <b>В очереди:</b> %d\n%s", position, FileLine(fileName))
}

func QueueStarted(fileName string) string {
	return "⚙️ <b>Конвертация началась</b>\n" + FileLine(fileName)
}

func TextReceivedChooseFormat() string {
	return "📝 <b>Текст получен</b>\nВыберите формат файла:"
}

func FileReceivedChooseFormat(fileName string) string {
	return "📥 <b>Файл получен</b>\n" + FileLine(fileName) + "\n\nВыберите формат для конвертации:"
}

func ErrorCannotDetectFileType(fileName string) string {
	return "🚫 <b>Не удалось определить тип файла</b>\n" + FileLine(fileName)
}

func ErrorCannotGetFormats() string {
	return "🚫 <b>Не удалось получить список форматов</b>\nПопробуйте ещё раз."
}

func EmptyTextHint() string {
	return "✍️ <b>Пустой текст</b>\nОтправьте текст, и я превращу его в файл."
}

func ErrorUploadTextAsFile() string {
	return "🚫 <b>Не удалось загрузить текст как файл</b>\nПопробуйте ещё раз."
}

func ErrorConversionFailed(fileName string, err error) string {
	msg := "🚫 <b>Ошибка конвертации</b>\n" + FileLine(fileName)
	if err != nil {
		msg += "\n\n" + fmt.Sprintf("<code>%s</code>", Escape(err.Error()))
	}
	return msg
}
