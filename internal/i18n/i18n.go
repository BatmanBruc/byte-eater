package i18n

import "strings"

type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

func FromLanguageCode(code string) Lang {
	code = strings.ToLower(strings.TrimSpace(code))
	if strings.HasPrefix(code, "ru") {
		return RU
	}
	return EN
}

func Parse(s string) Lang {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "ru":
		return RU
	case "en":
		return EN
	default:
		return EN
	}
}


