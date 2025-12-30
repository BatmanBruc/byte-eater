package pricing

import "strings"

func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

func isVideo(ext string) bool {
	switch ext {
	case "mp4", "avi", "wmv", "mkv", "3gp", "3gpp", "mpg", "mpeg", "webm", "ts", "mov", "flv", "asf", "vob":
		return true
	default:
		return false
	}
}

func isAudio(ext string) bool {
	switch ext {
	case "mp3", "ogg", "opus", "wav", "flac", "wma", "oga", "m4a", "aac", "aiff", "amr":
		return true
	default:
		return false
	}
}

func isImage(ext string) bool {
	switch ext {
	case "png", "jpg", "jpeg", "jp2", "webp", "bmp", "tif", "tiff", "gif", "ico", "heic", "avif", "tgs", "psd", "svg", "apng", "eps":
		return true
	default:
		return false
	}
}

func isEbook(ext string) bool {
	switch ext {
	case "epub", "mobi", "azw3", "lrf", "pdb", "cbr", "fb2", "cbz", "djvu":
		return true
	default:
		return false
	}
}

func isOffice(ext string) bool {
	switch ext {
	case "xlsx", "xls", "doc", "docx", "odt", "ods", "ppt", "pptx", "pptm", "pps", "ppsx", "ppsm", "pot", "potx", "potm", "odp":
		return true
	default:
		return false
	}
}

func Credits(originalExt, targetExt string, fileSize int64) (credits int, heavy bool) {
	// Все операции стоят 1 кредит, нет тяжелых операций
	return 1, false
}


