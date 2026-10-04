package main

import "log"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	store, err := OpenStore(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if cfg.StaffChatID == 0 {
		if savedChatID, loadErr := store.GetStaffChatID(); loadErr != nil {
			log.Fatal(loadErr)
		} else if savedChatID != 0 {
			cfg.StaffChatID = savedChatID
		}
	}

	bot, err := newTelegramBot(cfg.BotToken)
	if err != nil {
		log.Fatal(err)
	}
	service := NewBotService(bot, store, cfg)
	if err := service.Run(); err != nil {
		log.Fatal(err)
	}
}
