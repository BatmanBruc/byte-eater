package utils

import (
	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/go-telegram/bot/models"
)

func BuildInlineKeyboard(buttons []formats.FormatButton) models.InlineKeyboardMarkup {
	pad := func(s string) string { return " " + s + " " }
	rows := make([][]models.InlineKeyboardButton, 0)
	row := make([]models.InlineKeyboardButton, 0, 3)
	for i, button := range buttons {
		if i > 0 && i%3 == 0 {
			rows = append(rows, row)
			row = make([]models.InlineKeyboardButton, 0, 3)
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         pad(button.Text),
			CallbackData: button.CallbackData,
		})
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	return models.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}
