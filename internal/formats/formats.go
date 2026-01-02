package formats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BatmanBruc/bat-bot-convetor/internal/i18n"
	"github.com/BatmanBruc/bat-bot-convetor/internal/messages"
	"github.com/BatmanBruc/bat-bot-convetor/internal/pricing"
)

type FormatCategory struct {
	Name    string
	Icon    string
	Formats []string
}

type FormatButton struct {
	Text         string
	CallbackData string
	Credits      int
	Heavy        bool
}

type BatchFile struct {
	FileID   string
	FileName string
	FileSize int64
}

func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

func uniqUpper(formats []string) []string {
	seen := make(map[string]struct{}, len(formats))
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.ToUpper(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func withoutSameExt(targets []string, sourceExt string) []string {
	sourceExt = normalizeExt(sourceExt)
	if sourceExt == "" {
		return targets
	}
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if normalizeExt(t) == sourceExt {
			continue
		}
		out = append(out, t)
	}
	return out
}

func imageFormats() []string {
	return SupportedFormats["images"][0].Formats
}

func audioFormats() []string {
	return SupportedFormats["audio"][0].Formats
}

func videoFormats() []string {
	return SupportedFormats["video"][0].Formats
}

func ebookFormats() []string {
	return SupportedFormats["ebook"][0].Formats
}

func officeFormats() []string {
	return append(append(append([]string{}, writerFormats()...), sheetFormats()...), slideFormats()...)
}

func writerFormats() []string {
	return []string{"DOC", "DOCX", "ODT", "RTF", "TXT"}
}

func sheetFormats() []string {
	return []string{"XLS", "XLSX", "ODS"}
}

func slideFormats() []string {
	return []string{"PPT", "PPTX", "PPTM", "PPS", "PPSX", "PPSM", "POT", "POTX", "POTM", "ODP"}
}

func containsCaseInsensitive(list []string, item string) bool {
	item = normalizeExt(item)
	for _, v := range list {
		if normalizeExt(v) == item {
			return true
		}
	}
	return false
}

func GetTargetFormatsForSourceExt(sourceExt string) []string {
	sourceExt = normalizeExt(sourceExt)
	if sourceExt == "" {
		return nil
	}

	switch {
	case containsCaseInsensitive(imageFormats(), sourceExt):
		targets := uniqUpper(imageFormats())
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets

	case containsCaseInsensitive(audioFormats(), sourceExt):
		targets := uniqUpper(audioFormats())
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets

	case containsCaseInsensitive(videoFormats(), sourceExt):
		targets := append([]string{}, videoFormats()...)
		targets = append(targets, audioFormats()...)
		targets = append(targets, "GIF")
		targets = uniqUpper(targets)
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets

	case containsCaseInsensitive(ebookFormats(), sourceExt):
		targets := append([]string{}, ebookFormats()...)
		targets = append(targets, "PDF")
		targets = uniqUpper(targets)
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets
	}

	if sourceExt == "pdf" {
		return []string{"TXT"}
	}

	if containsCaseInsensitive(writerFormats(), sourceExt) {
		targets := append([]string{}, writerFormats()...)
		targets = append(targets, "PDF")
		targets = uniqUpper(targets)
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets
	}

	if containsCaseInsensitive(sheetFormats(), sourceExt) {
		targets := append([]string{}, sheetFormats()...)
		targets = append(targets, "PDF")
		targets = uniqUpper(targets)
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets
	}

	if containsCaseInsensitive(slideFormats(), sourceExt) {
		targets := append([]string{}, slideFormats()...)
		targets = append(targets, "PDF")
		targets = uniqUpper(targets)
		targets = withoutSameExt(targets, sourceExt)
		sort.Strings(targets)
		return targets
	}

	return nil
}

func GetFormatButtonsBySourceExt(sourceExt string, taskID string) []FormatButton {
	return GetFormatButtonsByList(GetTargetFormatsForSourceExt(sourceExt), taskID)
}

func GetFormatButtonsBySourceExtWithCredits(sourceExt string, taskID string, fileSize int64) []FormatButton {
	targets := GetTargetFormatsForSourceExt(sourceExt)
	buttons := make([]FormatButton, 0, len(targets))
	for _, t := range targets {
		credits, heavy := pricing.Credits(sourceExt, t, fileSize)
		text := t
		if credits > 0 {
			if heavy {
				text = t + " " + "★" + " " + fmt.Sprintf("(%d)", credits)
			} else {
				text = t + " " + fmt.Sprintf("(%d)", credits)
			}
		}
		buttons = append(buttons, FormatButton{
			Text:         text,
			CallbackData: strings.ToLower(t) + "_for_" + taskID,
			Credits:      credits,
			Heavy:        heavy,
		})
	}
	return buttons
}

func pick(lang i18n.Lang, ru string, en string) string {
	if lang == i18n.RU {
		return ru
	}
	return en
}

func GetButtonsForSourceExtWithCredits(sourceExt string, taskID string, fileSize int64, lang i18n.Lang) []FormatButton {
	sourceExt = normalizeExt(sourceExt)
	if containsCaseInsensitive(imageFormats(), sourceExt) {
		return getImageActionButtonsWithCredits(sourceExt, taskID, fileSize, lang)
	}
	if containsCaseInsensitive(videoFormats(), sourceExt) {
		return getVideoActionButtonsWithCredits(sourceExt, taskID, fileSize, lang)
	}
	if sourceExt == "pdf" {
		return getPdfActionButtonsWithCredits(taskID, fileSize, lang)
	}
	return GetFormatButtonsBySourceExtWithCredits(sourceExt, taskID, fileSize)
}

func formatButtonText(label string, credits int, heavy bool) string {
	// Все операции стоят 1 кредит, не показываем стоимость
	return strings.TrimSpace(label)
}

func getImageActionButtonsWithCredits(sourceExt string, taskID string, fileSize int64, lang i18n.Lang) []FormatButton {
	buttons := make([]FormatButton, 0)
	{
		credits, heavy := pricing.Credits(sourceExt, "jpg", fileSize)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "🛒 Авито (JPG)", "🛒 Avito (JPG)"), credits, heavy),
			CallbackData: fmt.Sprintf("pimg_avito_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "📸 Instagram пост 1080×1080", "📸 Instagram post 1080×1080"), credits, heavy),
			CallbackData: fmt.Sprintf("pimg_instagram_feed_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "📲 Instagram сторис 1080×1920", "📲 Instagram story 1080×1920"), credits, heavy),
			CallbackData: fmt.Sprintf("pimg_instagram_story_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "🟦 VK пост 1080×1080", "🟦 VK post 1080×1080"), credits, heavy),
			CallbackData: fmt.Sprintf("pimg_vk_square_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}
	buttons = append(buttons, GetFormatButtonsBySourceExtWithCredits(sourceExt, taskID, fileSize)...)

	resizePresets := []int{1080, 720}
	for _, max := range resizePresets {
		credits, heavy := pricing.Credits(sourceExt, sourceExt, fileSize)
		label := fmt.Sprintf("%s %dpx", pick(lang, "📏", "📏"), max)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, credits, heavy),
			CallbackData: fmt.Sprintf("imgr_%s_%d_for_%s", strings.ToLower(sourceExt), max, taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}

	compressTargets := []struct {
		ext     string
		quality int
	}{
		{"jpg", 85},
		{"jpg", 70},
		{"webp", 85},
		{"webp", 70},
	}
	for _, ct := range compressTargets {
		credits, heavy := pricing.Credits(sourceExt, ct.ext, fileSize)
		label := fmt.Sprintf("%s %s %d%%", pick(lang, "🗜", "🗜"), strings.ToUpper(ct.ext), ct.quality)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, credits, heavy),
			CallbackData: fmt.Sprintf("imgc_%s_%d_for_%s", strings.ToLower(ct.ext), ct.quality, taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}

	return buttons
}

func getVideoActionButtonsWithCredits(sourceExt string, taskID string, fileSize int64, lang i18n.Lang) []FormatButton {
	buttons := make([]FormatButton, 0)
	{
		credits, heavy := pricing.Credits(sourceExt, "mp4", fileSize)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "🎵 TikTok 9:16 1080×1920", "🎵 TikTok 9:16 1080×1920"), credits, heavy),
			CallbackData: fmt.Sprintf("pvid_tiktok_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "📲 Reels 9:16 1080×1920", "📲 Reels 9:16 1080×1920"), credits, heavy),
			CallbackData: fmt.Sprintf("pvid_reels_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "▶️ Shorts 9:16 1080×1920", "▶️ Shorts 9:16 1080×1920"), credits, heavy),
			CallbackData: fmt.Sprintf("pvid_shorts_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "🟦 VK Clips 9:16 1080×1920", "🟦 VK Clips 9:16 1080×1920"), credits, heavy),
			CallbackData: fmt.Sprintf("pvid_vk_clips_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(pick(lang, "📺 YouTube 16:9 1920×1080", "📺 YouTube 16:9 1920×1080"), credits, heavy),
			CallbackData: fmt.Sprintf("pvid_youtube_1080p_for_%s", taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}
	buttons = append(buttons, GetFormatButtonsBySourceExtWithCredits(sourceExt, taskID, fileSize)...)

	resizeHeights := []int{720, 480}
	for _, h := range resizeHeights {
		credits, heavy := pricing.Credits(sourceExt, "mp4", fileSize)
		label := fmt.Sprintf("%s %dp", pick(lang, "📏", "📏"), h)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, credits, heavy),
			CallbackData: fmt.Sprintf("vidr_mp4_%d_for_%s", h, taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}

	crfPresets := []int{28, 35}
	for _, crf := range crfPresets {
		credits, heavy := pricing.Credits(sourceExt, "mp4", fileSize)
		label := fmt.Sprintf("%s MP4 CRF %d", pick(lang, "🗜", "🗜"), crf)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, credits, heavy),
			CallbackData: fmt.Sprintf("vidc_mp4_%d_for_%s", crf, taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}

	gifHeights := []int{480, 320}
	for _, h := range gifHeights {
		credits, heavy := pricing.Credits(sourceExt, "gif", fileSize)
		label := fmt.Sprintf("%s GIF %dp", pick(lang, "🎞", "🎞"), h)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, credits, heavy),
			CallbackData: fmt.Sprintf("vidg_gif_%d_for_%s", h, taskID),
			Credits:      credits,
			Heavy:        heavy,
		})
	}

	return buttons
}

func getPdfActionButtonsWithCredits(taskID string, fileSize int64, lang i18n.Lang) []FormatButton {
	buttons := make([]FormatButton, 0)
	{
		credits, heavy := pricing.Credits("pdf", "txt", fileSize)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText("TXT", credits, heavy),
			CallbackData: "txt_for_" + taskID,
			Credits:      credits,
			Heavy:        heavy,
		})
	}
	return buttons
}

func GetBatchButtonsBySourceExtWithCredits(sourceExt string, taskID string, files []BatchFile, lang i18n.Lang) []FormatButton {
	sourceExt = normalizeExt(sourceExt)
	if len(files) == 0 {
		return nil
	}

	targets := GetTargetFormatsForSourceExt(sourceExt)
	if len(targets) == 0 {
		return nil
	}

	buttons := make([]FormatButton, 0, len(targets))
	for _, t := range targets {
		total := 0
		heavyAny := false
		for _, f := range files {
			c, h := pricing.Credits(sourceExt, t, f.FileSize)
			total += c
			if h {
				heavyAny = true
			}
		}
		label := strings.ToUpper(t)
		buttons = append(buttons, FormatButton{
			Text:         formatButtonText(label, total, heavyAny),
			CallbackData: strings.ToLower(t) + "_for_" + taskID,
			Credits:      total,
			Heavy:        heavyAny,
		})
	}
	return buttons
}

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

func GetTextOutputButtons(taskID string) []FormatButton {
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

func GetHelpMessage(lang i18n.Lang) string {
	var msg strings.Builder
	msg.WriteString(messages.HelpHeader(lang))
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

	if lang == i18n.RU {
	msg.WriteString("🧭 <b>Использование</b>\n")
		msg.WriteString("1) Отправьте файл/текст (также можно войсы и кружки)\n")
		msg.WriteString("2) Выберите целевой формат в кнопках (список зависит от исходного формата)\n")
	msg.WriteString("3) Дождитесь результата\n\n")
		msg.WriteString("Примеры:\n")
	} else {
		msg.WriteString("🧭 <b>How to use</b>\n")
		msg.WriteString("1) Send a file/text (voice messages and video notes also work)\n")
		msg.WriteString("2) Choose the target format from buttons (options depend on the source format)\n")
		msg.WriteString("3) Wait for the result\n\n")
		msg.WriteString("Examples:\n")
	}
	msg.WriteString("• <code>.docx → PDF/TXT</code>\n")
	msg.WriteString("• <code>.pdf → TXT</code>\n")
	msg.WriteString("• <code>.mp4 → avi/mkv</code>")

	return msg.String()
}
