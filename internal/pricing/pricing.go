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
	orig := normalizeExt(originalExt)
	targ := normalizeExt(targetExt)

	if orig == "" || targ == "" {
		return 0, true
	}

	if orig == targ {
		return 1, false
	}

	if orig == "pdf" && targ == "txt" {
		return 1, false
	}
	if orig == "pdf" && targ == "zip" {
		if fileSize >= 50*1024*1024 {
			return 12, true
		}
		return 7, true
	}

	if isVideo(orig) {
		if targ == "gif" {
			if fileSize >= 50*1024*1024 {
				return 18, true
			}
			return 12, true
		}
		if isVideo(targ) {
			if fileSize >= 200*1024*1024 {
				return 14, true
			}
			return 9, true
		}
		if isAudio(targ) {
			if fileSize >= 100*1024*1024 {
				return 8, true
			}
			return 4, false
		}
	}

	if isAudio(orig) && isAudio(targ) {
		if fileSize >= 100*1024*1024 {
			return 6, true
		}
		return 3, false
	}

	if isImage(orig) && isImage(targ) {
		if orig == "tif" || orig == "tiff" || orig == "psd" || orig == "heic" || orig == "avif" || orig == "tgs" {
			return 7, true
		}
		if targ == "tif" || targ == "tiff" || targ == "psd" {
			return 6, true
		}
		if fileSize >= 30*1024*1024 {
			return 5, true
		}
		return 2, false
	}

	if isOffice(orig) || isOffice(targ) || orig == "rtf" || orig == "txt" || targ == "rtf" || targ == "txt" {
		if fileSize >= 50*1024*1024 {
			return 9, true
		}
		return 6, true
	}

	if isEbook(orig) || isEbook(targ) || targ == "pdf" && isEbook(orig) {
		if fileSize >= 50*1024*1024 {
			return 10, true
		}
		return 7, true
	}

	if fileSize >= 100*1024*1024 {
		return 6, true
	}

	return 2, false
}


