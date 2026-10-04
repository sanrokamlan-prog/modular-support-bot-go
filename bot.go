package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotService struct {
	bot       *tgbotapi.BotAPI
	store     *Store
	cfg       Config
	mu        sync.Mutex
	userTimes map[int64][]time.Time
	threads   map[string]int
}

func NewBotService(bot *tgbotapi.BotAPI, store *Store, cfg Config) *BotService {
	return &BotService{bot: bot, store: store, cfg: cfg, userTimes: make(map[int64][]time.Time), threads: make(map[string]int)}
}

func (s *BotService) Run() error {
	log.Printf("support bot started as @%s", s.bot.Self.UserName)
	offset := 0
	for {
		response, err := s.bot.MakeRequest("getUpdates", tgbotapi.Params{
			"offset":  fmt.Sprintf("%d", offset),
			"timeout": "50",
		})
		if err != nil {
			return err
		}
		var updates []rawUpdate
		if err := json.Unmarshal(response.Result, &updates); err != nil {
			return err
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if len(update.Message) == 0 {
				continue
			}
			var message tgbotapi.Message
			if err := json.Unmarshal(update.Message, &message); err != nil {
				log.Printf("decode update %d: %v", update.UpdateID, err)
				continue
			}
			var metadata struct {
				MessageThreadID int `json:"message_thread_id"`
			}
			if json.Unmarshal(update.Message, &metadata) == nil && metadata.MessageThreadID != 0 && message.Chat != nil {
				s.threads[threadKey(message.Chat.ID, message.MessageID)] = metadata.MessageThreadID
			}
			if err := s.handleMessage(&message); err != nil {
				log.Printf("handle update %d: %v", update.UpdateID, err)
			}
		}
	}
}

type rawUpdate struct {
	UpdateID int             `json:"update_id"`
	Message  json.RawMessage `json:"message"`
}

func (s *BotService) handleMessage(message *tgbotapi.Message) error {
	if message == nil || message.Chat == nil {
		return nil
	}
	if message.Chat.IsPrivate() {
		return s.handleUserMessage(message)
	}
	if message.From == nil || !s.cfg.StaffIDs[message.From.ID] {
		return nil
	}
	if s.cfg.StaffChatID == 0 {
		if !message.Chat.IsSuperGroup() {
			return nil
		}
		if err := s.store.SetStaffChatID(message.Chat.ID); err != nil {
			return err
		}
		s.SetStaffChatID(message.Chat.ID)
		if _, err := s.bot.Send(tgbotapi.NewMessage(message.Chat.ID, "✅ 已识别为客服超级群组。请确认已开启 Topics，之后用户工单会自动创建独立话题。")); err != nil {
			return err
		}
	}
	if message.Chat.ID != s.cfg.StaffChatID {
		return nil
	}
	return s.handleStaffMessage(message)
}

func (s *BotService) handleUserMessage(message *tgbotapi.Message) error {
	if message.From == nil || message.From.IsBot {
		return nil
	}
	if banned, err := s.store.IsBanned(message.From.ID); err != nil {
		return err
	} else if banned {
		return nil
	}
	if message.IsCommand() && strings.EqualFold(message.Command(), "start") {
		_, err := s.requireVerification(message)
		return err
	}
	verified, err := s.requireVerification(message)
	if err != nil || !verified {
		return err
	}
	if s.cfg.StaffChatID == 0 {
		return s.sendUser(message.Chat.ID, "机器人已经接入，但管理员还没有配置客服超级群组。")
	}
	if !s.allowUser(message.From.ID) {
		return s.sendUser(message.Chat.ID, "发送太频繁了，请稍等几分钟后再试。")
	}

	ticket, err := s.store.OpenTicket(message.From.ID)
	if err != nil {
		return err
	}
	if ticket.ID == 0 {
		ticket, err = s.store.CreateTicket(message.From.ID, message.From.FirstName)
		if err != nil {
			return err
		}
		threadID, topicErr := s.createTopic(ticket)
		if topicErr != nil {
			return topicErr
		}
		ticket.ThreadID = threadID
	}
	body := messageText(message)
	if body == "" {
		return s.sendUser(message.Chat.ID, "目前请先发送文字内容。")
	}
	if err := s.store.AddMessage(ticket.ID, "user", body); err != nil {
		return err
	}
	if err := s.forwardToStaff(ticket, body); err != nil {
		return err
	}
	return s.sendUser(message.Chat.ID, "已收到你的工单，客服会在这里回复。")
}

func (s *BotService) SetStaffChatID(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.StaffChatID = chatID
}

func (s *BotService) requireVerification(message *tgbotapi.Message) (bool, error) {
	userID := message.From.ID
	v, err := s.store.GetVerification(userID)
	if err != nil {
		return false, err
	}
	now := time.Now()
	if v.Verified {
		return true, nil
	}
	if v.LockedUntil.After(now) {
		remaining := int(time.Until(v.LockedUntil).Minutes()) + 1
		return false, s.sendUser(message.Chat.ID, fmt.Sprintf("验证失败次数过多，请 %d 分钟后再试。", remaining))
	}
	answerText := strings.TrimSpace(messageText(message))
	if v.Question == "" || !v.ExpiresAt.After(now) {
		c, challengeErr := newChallenge()
		if challengeErr != nil {
			return false, challengeErr
		}
		if err := s.store.SaveChallenge(userID, c.Question, c.Answer, now.Add(time.Duration(s.cfg.ChallengeTTL)*time.Second)); err != nil {
			return false, err
		}
		return false, s.sendUser(message.Chat.ID, "开始前请先完成验证。请计算：\n\n"+c.Question+"\n\n直接回复数字即可（15 分钟内有效）。")
	}
	if answerText != strconv.Itoa(v.Answer) {
		attempts := v.Attempts + 1
		lockedUntil := time.Unix(0, 0)
		if attempts >= s.cfg.MaxAttempts {
			lockedUntil = now.Add(time.Duration(s.cfg.LockDuration) * time.Second)
		}
		if err := s.store.RecordFailure(userID, attempts, lockedUntil); err != nil {
			return false, err
		}
		if lockedUntil.After(now) {
			return false, s.sendUser(message.Chat.ID, "验证失败次数过多，暂时锁定，请稍后再试。")
		}
		return false, s.sendUser(message.Chat.ID, fmt.Sprintf("答案不正确，还可以尝试 %d 次。", s.cfg.MaxAttempts-attempts))
	}
	if err := s.store.MarkVerified(userID); err != nil {
		return false, err
	}
	return false, s.sendUser(message.Chat.ID, "验证通过。请重新发送你的问题来创建工单。")
}

func (s *BotService) createTopic(ticket Ticket) (int, error) {
	name := fmt.Sprintf("#T%06d · %s", ticket.ID, truncate(ticket.FirstName, 80))
	result, err := s.bot.MakeRequest("createForumTopic", tgbotapi.Params{
		"chat_id": fmt.Sprintf("%d", s.cfg.StaffChatID),
		"name":    name,
	})
	if err != nil {
		return 0, fmt.Errorf("create forum topic: %w", err)
	}
	var topic struct {
		MessageThreadID int `json:"message_thread_id"`
	}
	if err := json.Unmarshal(result.Result, &topic); err != nil || topic.MessageThreadID == 0 {
		return 0, fmt.Errorf("create forum topic returned no thread id")
	}
	if err := s.store.SetThreadID(ticket.ID, topic.MessageThreadID); err != nil {
		return 0, err
	}
	return topic.MessageThreadID, nil
}

func (s *BotService) forwardToStaff(ticket Ticket, body string) error {
	text := fmt.Sprintf("工单 #T%06d\n来自：%s (%d)\n\n%s", ticket.ID, ticket.FirstName, ticket.UserID, body)
	response, err := s.bot.MakeRequest("sendMessage", tgbotapi.Params{
		"chat_id":           fmt.Sprintf("%d", s.cfg.StaffChatID),
		"text":              text,
		"message_thread_id": fmt.Sprintf("%d", ticket.ThreadID),
	})
	if err == nil {
		var sent tgbotapi.Message
		if json.Unmarshal(response.Result, &sent) == nil {
			s.threads[threadKey(s.cfg.StaffChatID, sent.MessageID)] = ticket.ThreadID
		}
	}
	return err
}

func (s *BotService) handleStaffMessage(message *tgbotapi.Message) error {
	threadID := s.threadID(message)
	if threadID == 0 {
		return nil
	}
	ticket, err := s.store.TicketByThread(threadID)
	if err != nil {
		return err
	}
	if ticket.ID == 0 || ticket.Status != "open" {
		return nil
	}
	body := strings.TrimSpace(messageText(message))
	if body == "" {
		return nil
	}
	if message.IsCommand() {
		switch strings.ToLower(message.Command()) {
		case "close":
			if err := s.store.CloseTicket(ticket.ID); err != nil {
				return err
			}
			return s.sendUser(ticket.UserID, fmt.Sprintf("工单 #T%06d 已关闭。需要帮助时可以重新发送消息。", ticket.ID))
		case "ban":
			if err := s.store.BanUser(ticket.UserID); err != nil {
				return err
			}
			if err := s.store.CloseTicket(ticket.ID); err != nil {
				return err
			}
			return s.sendUser(ticket.UserID, "此会话已被限制。")
		}
	}
	if err := s.store.AddMessage(ticket.ID, "staff", body); err != nil {
		return err
	}
	return s.sendUser(ticket.UserID, fmt.Sprintf("客服：\n\n%s", body))
}

func (s *BotService) sendUser(userID int64, text string) error {
	_, err := s.bot.Send(tgbotapi.NewMessage(userID, text))
	return err
}

func (s *BotService) allowUser(userID int64) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-time.Duration(s.cfg.RateLimitWindow) * time.Second)
	items := s.userTimes[userID]
	kept := items[:0]
	for _, item := range items {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	if len(kept) >= s.cfg.RateLimitCount {
		s.userTimes[userID] = kept
		return false
	}
	s.userTimes[userID] = append(kept, now)
	return true
}

func messageText(message *tgbotapi.Message) string {
	if message == nil {
		return ""
	}
	if message.Text != "" {
		return message.Text
	}
	return message.Caption
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func threadKey(chatID int64, messageID int) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func (s *BotService) threadID(message *tgbotapi.Message) int {
	if message == nil || message.Chat == nil {
		return 0
	}
	if message.ReplyToMessage != nil {
		if id := s.threads[threadKey(message.Chat.ID, message.ReplyToMessage.MessageID)]; id != 0 {
			return id
		}
	}
	return s.threads[threadKey(message.Chat.ID, message.MessageID)]
}
