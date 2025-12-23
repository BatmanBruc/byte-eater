package formats

import (
	"fmt"
	"strings"

	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
)

type FormatCategory struct {
	Name    string
	Icon    string
	Formats []string
}

type FormatButton struct {
	Text         string
	CallbackData string
}

// GetFormatButtonsByList формирует кнопки inline-клавиатуры из явного списка форматов.
// Форматы показываются как есть, а callback data имеет вид "<lowercase_format>_for_<taskID>".
func GetFormatButtonsByList(formatList []string, taskID string) []FormatButton {
	buttons := make([]FormatButton, 0, len(formatList))
	for _, format := range formatList {
		format = strings.TrimSpace(format)
		if format == "" {
			continue
		}
		formatLower := strings.ToLower(format)
		buttons = append(buttons, FormatButton{
			Text:         format,
			CallbackData: formatLower + "_for_" + taskID,
		})
	}
	return buttons
}

// GetTextOutputButtons возвращает кнопки форматов вывода для сценария "текстовое сообщение -> файл".
func GetTextOutputButtons(taskID string) []FormatButton {
	// Держим список консервативным: форматы, которые LibreOffice обычно генерирует стабильно.
	return GetFormatButtonsByList([]string{"TXT", "PDF", "DOCX", "RTF", "ODT"}, taskID)
}

var SupportedFormats = map[string][]FormatCategory{
	"images": {
		{
			Name:    "Images",
			Icon:    "📷",
			Formats: []string{"PNG", "JPG", "JPEG", "JP2", "WEBP", "BMP", "TIF", "TIFF", "GIF", "ICO", "HEIC", "AVIF", "TGS", "PSD", "SVG", "APNG", "EPS"},
		},
	},
	"audio": {
		{
			Name:    "Audio",
			Icon:    "🔊",
			Formats: []string{"MP3", "OGG", "OPUS", "WAV", "FLAC", "WMA", "OGA", "M4A", "AAC", "AIFF", "AMR"},
		},
	},
	"video": {
		{
			Name:    "Video",
			Icon:    "📹",
			Formats: []string{"MP4", "AVI", "WMV", "MKV", "3GP", "3GPP", "MPG", "MPEG", "WEBM", "TS", "MOV", "FLV", "ASF", "VOB"},
		},
	},
	"document": {
		{
			Name:    "Document",
			Icon:    "💼",
			Formats: []string{"XLSX", "XLS", "TXT", "RTF", "DOC", "DOCX", "ODT", "PDF", "ODS", "TORRENT"},
		},
	},
	"presentation": {
		{
			Name:    "Presentation",
			Icon:    "🖼",
			Formats: []string{"PPT", "PPTX", "PPTM", "PPS", "PPSX", "PPSM", "POT", "POTX", "POTM", "ODP"},
		},
	},
	"ebook": {
		{
			Name:    "eBook",
			Icon:    "📚",
			Formats: []string{"EPUB", "MOBI", "AZW3", "LRF", "PDB", "CBR", "FB2", "CBZ", "DJVU"},
		},
	},
	"font": {
		{
			Name:    "Font",
			Icon:    "🔤",
			Formats: []string{"TTF", "OTF", "EOT", "WOFF", "WOFF2", "SVG", "PFB"},
		},
	},
}

func GetAllFormats() []FormatCategory {
	var all []FormatCategory
	for _, categories := range SupportedFormats {
		all = append(all, categories...)
	}
	return all
}

func FormatExists(format string) bool {
	formatUpper := strings.ToUpper(format)
	for _, categories := range SupportedFormats {
		for _, category := range categories {
			for _, f := range category.Formats {
				if strings.ToUpper(f) == formatUpper {
					return true
				}
			}
		}
	}
	return false
}

func GetCategoryByExtension(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))

	extToCategory := map[string]string{
		"png": "images", "jpg": "images", "jpeg": "images", "jp2": "images",
		"webp": "images", "bmp": "images", "tif": "images", "tiff": "images",
		"gif": "images", "ico": "images", "heic": "images", "avif": "images",
		"tgs": "images", "psd": "images", "svg": "images", "apng": "images",
		"eps": "images",

		"mp3": "audio", "ogg": "audio", "opus": "audio", "wav": "audio",
		"flac": "audio", "wma": "audio", "oga": "audio", "m4a": "audio",
		"aac": "audio", "aiff": "audio", "amr": "audio",

		"mp4": "video", "avi": "video", "wmv": "video", "mkv": "video",
		"3gp": "video", "3gpp": "video", "mpg": "video", "mpeg": "video",
		"webm": "video", "ts": "video", "mov": "video", "flv": "video",
		"asf": "video", "vob": "video",

		"xlsx": "document", "xls": "document", "txt": "document",
		"rtf": "document", "doc": "document", "docx": "document",
		"odt": "document", "pdf": "document", "ods": "document",
		"torrent": "document",

		"ppt": "presentation", "pptx": "presentation", "pptm": "presentation",
		"pps": "presentation", "ppsx": "presentation", "ppsm": "presentation",
		"pot": "presentation", "potx": "presentation", "potm": "presentation",
		"odp": "presentation",

		"epub": "ebook", "mobi": "ebook", "azw3": "ebook", "lrf": "ebook",
		"pdb": "ebook", "cbr": "ebook", "fb2": "ebook", "cbz": "ebook",
		"djvu": "ebook",

		"ttf": "font", "otf": "font", "eot": "font", "woff": "font",
		"woff2": "font", "pfb": "font",
	}

	if category, ok := extToCategory[ext]; ok {
		return category
	}

	return ""
}

func GetFormatButtonsByCategory(category string, taskID string) []FormatButton {
	categories, ok := SupportedFormats[category]
	if !ok || len(categories) == 0 {
		return []FormatButton{}
	}

	formats := categories[0].Formats
	return GetFormatButtonsByList(formats, taskID)
}

func GetHelpMessage() string {
	var msg strings.Builder
	msg.WriteString(messages.HelpHeader())
	msg.WriteString("\n")

	categories := []struct {
		key  string
		name string
	}{
		{"images", "📷 Images"},
		{"audio", "🔊 Audio"},
		{"video", "📹 Video"},
		{"document", "💼 Document"},
		{"presentation", "🖼 Presentation"},
		{"ebook", "📚 eBook"},
		{"font", "🔤 Font"},
	}

	for _, cat := range categories {
		if formats, ok := SupportedFormats[cat.key]; ok && len(formats) > 0 {
			msg.WriteString(fmt.Sprintf("• <b>%s</b> <i>(%d)</i>\n", messages.Escape(cat.name), len(formats[0].Formats)))
			msg.WriteString("<code>")
			msg.WriteString(messages.Escape(strings.Join(formats[0].Formats, ", ")))
			msg.WriteString("</code>\n\n")
		}
	}

	msg.WriteString("🧭 <b>Использование</b>\n")
	msg.WriteString("1) Отправьте файл или текст\n")
	msg.WriteString("2) Выберите целевой формат в кнопках\n")
	msg.WriteString("3) Дождитесь результата\n\n")
	msg.WriteString("Пример: <code>.docx → PDF</code>")

	return msg.String()
}
