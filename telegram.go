package main

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func newTelegramBot(token string) (*tgbotapi.BotAPI, error) {
	return tgbotapi.NewBotAPI(token)
}
