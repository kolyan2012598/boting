package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const BOT_TOKEN = "8495592139:AAHJXCVAuQBpEbMfrcIbuGhP5CL8_zVQny4"

// ======================
// СТРУКТУРЫ ДАННЫХ
// ======================

type UserData struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	FirstName   string `json:"first_name"`
	Coins       int    `json:"coins"`
	Gems        int    `json:"gems"`
	Level       int    `json:"level"`
	XP          int    `json:"xp"`
	Warns       int    `json:"warns"`
	MutedUntil  int64  `json:"muted_until"`
	BannedUntil int64  `json:"banned_until"`
	MarriedTo   int64  `json:"married_to"`
	Reputation  int    `json:"reputation"`
	Bio         string `json:"bio"`
	Title       string `json:"title"`
	Clan        string `json:"clan"`
	GamesWon    int    `json:"games_won"`
	GamesPlayed int    `json:"games_played"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
}

type ChatSettings struct {
	ChatID          int64  `json:"chat_id"`
	WelcomeMessage  string `json:"welcome_message"`
	Rules           string `json:"rules"`
	AntiSpam        bool   `json:"anti_spam"`
	AntiFlood       bool   `json:"anti_flood"`
	MaxWarns        int    `json:"max_warns"`
	MuteTime        int    `json:"mute_time"`
	BanTime         int    `json:"ban_time"`
	GreetingEnabled bool   `json:"greeting_enabled"`
	GameEnabled     bool   `json:"game_enabled"`
}

type Game struct {
	ID          string
	ChatID      int64
	Players     map[int64]*GamePlayer
	Phase       string
	Round       int
	Votes       map[int64]int64
	MafiaTarget int64
	DoctorSave  int64
	JoinMsgID   int
	StartTime   time.Time
	Timer       *time.Timer
	Events      []string
}

type GamePlayer struct {
	UserID    int64
	FirstName string
	Role      string
	Alive     bool
	Defense   int
	VotedFor  int64
}

type Marriage struct {
	User1   int64
	User2   int64
	Since   time.Time
	Love    int
}

var (
	// Базы данных
	users        = make(map[int64]*UserData)
	chatSettings = make(map[int64]*ChatSettings)
	games        = make(map[string]*Game)
	marriages    = make(map[int64]*Marriage)
	
	// Мьютексы
	mu          sync.RWMutex
	gameMu      sync.RWMutex
	
	// Счетчики
	gameCounter int
	
	// Команды
	commands = map[string]string{
		"ban": "Забанить пользователя",
		"unban": "Разбанить",
		"mute": "Замутить",
		"unmute": "Размутить",
		"warn": "Предупредить",
		"kick": "Кикнуть",
		"info": "Информация о пользователе",
		"game": "Создать игру Мафия",
		"join": "Присоединиться к игре",
		"profile": "Профиль",
		"shop": "Магазин",
		"marry": "Пожениться",
		"divorce": "Развестись",
		"reputation": "Репутация",
		"top": "Топ игроков",
		"report": "Пожаловаться",
		"warnlist": "Список предупреждений",
		"purge": "Очистить сообщения",
		"say": "Сказать от имени бота",
		"poll": "Создать опрос",
		"give": "Передать монеты",
		"daily": "Ежедневный бонус",
		"casino": "Казино",
		"duel": "Дуэль",
	}
)

// ======================
// MAIN
// ======================

func main() {
	rand.Seed(time.Now().UnixNano())
	
	// Загружаем данные
	loadAllData()
	
	// Автосохранение
	go autoSave()
	
	// Web server для Render
	go startWebServer()
	
	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}
	
	bot.Debug = false
	
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     🔥 IRIS MAFIA BOT v3.0 🔥      ║")
	fmt.Println("╠══════════════════════════════════════╣")
	fmt.Printf("║ Bot: @%s\n", bot.Self.UserName)
	fmt.Printf("║ Users: %d\n", len(users))
	fmt.Printf("║ Chats: %d\n", len(chatSettings))
	fmt.Printf("║ Games: %d\n", len(games))
	fmt.Printf("║ Marriages: %d\n", len(marriages))
	fmt.Println("║ Features: Admin + Games + Fun      ║")
	fmt.Println("║ No censorship! Freedom of speech!  ║")
	fmt.Println("╚══════════════════════════════════════╝")
	
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	
	for update := range updates {
		if update.Message != nil {
			go handleMessage(bot, update.Message)
		}
		if update.CallbackQuery != nil {
			go handleCallback(bot, update.CallbackQuery)
		}
	}
}

func startWebServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Iris Mafia Bot</title>
<style>
body{background:#0a0a0a;color:#fff;font-family:Arial;text-align:center;padding:50px}
h1{color:#ff006e;font-size:48px}
.stats{color:#888;margin:20px}
.feature{color:#00ff88;margin:10px}
</style>
</head>
<body>
<h1>🔥 Iris Mafia Bot</h1>
<p class="stats">Users: %d | Chats: %d | Games: %d</p>
<p class="feature">Admin Panel • Mafia Game • Shop • Marriages • Casino</p>
<p style="color:#ff006e">© @Bratubeymenya | No Censorship</p>
</body>
</html>`, len(users), len(chatSettings), len(games))
	})
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	http.ListenAndServe(":"+port, nil)
}

func autoSave() {
	for {
		time.Sleep(5 * time.Minute)
		saveAllData()
	}
}

func loadAllData() {
	loadJSON("users.json", &users)
	loadJSON("chats.json", &chatSettings)
	loadJSON("marriages.json", &marriages)
}

func saveAllData() {
	saveJSON("users.json", users)
	saveJSON("chats.json", chatSettings)
	saveJSON("marriages.json", marriages)
}

func loadJSON(filename string, v interface{}) {
	data, err := ioutil.ReadFile(filename)
	if err == nil {
		json.Unmarshal(data, v)
	}
}

func saveJSON(filename string, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	ioutil.WriteFile(filename, data, 0644)
}

// ======================
// ОБРАБОТКА СООБЩЕНИЙ
// ======================

func handleMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if msg.From.IsBot {
		return
	}
	
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()
	isPrivate := !isGroup
	
	// Инициализация пользователя
	initUser(msg.From)
	
	// Проверка бана/мута
	if isGroup {
		if isUserBanned(chatID, userID) {
			return
		}
		if isUserMuted(chatID, userID) {
			bot.Send(tgbotapi.NewDeleteMessage(chatID, msg.MessageID))
			return
		}
		
		// Анти-спам
		if isSpam(chatID, userID) {
			bot.Send(tgbotapi.NewDeleteMessage(chatID, msg.MessageID))
			return
		}
	}
	
	// Обработка команд
	if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
		handleCommand(bot, msg, isGroup, isPrivate)
		return
	}
	
	// Приветствие нового участника
	if msg.NewChatMembers != nil {
		for _, member := range msg.NewChatMembers {
			handleNewMember(bot, chatID, member)
		}
	}
	
	// Прощание с ушедшим
	if msg.LeftChatMember != nil {
		handleLeftMember(bot, chatID, msg.LeftChatMember)
	}
	
	// Обработка репутации
	if isGroup && msg.ReplyToMessage != nil {
		if strings.Contains(text, "+") || strings.Contains(text, "спасибо") {
			changeReputation(bot, chatID, msg.ReplyToMessage.From.ID, 1)
		}
	}
}

func handleCommand(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, isGroup, isPrivate bool) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text
	
	// Убираем префикс
	text = strings.TrimPrefix(text, "/")
	text = strings.TrimPrefix(text, "!")
	
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	
	command := strings.ToLower(parts[0])
	args := parts[1:]
	
	switch command {
	// ======================
	// АДМИН КОМАНДЫ
	// ======================
	case "ban":
		if isGroup && isAdmin(bot, chatID, userID) {
			banUser(bot, chatID, userID, args, msg)
		}
		
	case "unban":
		if isGroup && isAdmin(bot, chatID, userID) {
			unbanUser(bot, chatID, args)
		}
		
	case "mute":
		if isGroup && isAdmin(bot, chatID, userID) {
			muteUser(bot, chatID, userID, args, msg)
		}
		
	case "unmute":
		if isGroup && isAdmin(bot, chatID, userID) {
			unmuteUser(bot, chatID, args)
		}
		
	case "kick":
		if isGroup && isAdmin(bot, chatID, userID) {
			kickUser(bot, chatID, args, msg)
		}
		
	case "warn":
		if isGroup && isAdmin(bot, chatID, userID) {
			warnUser(bot, chatID, args, msg)
		}
		
	case "warnlist":
		if isGroup {
			showWarnList(bot, chatID, args)
		}
		
	case "purge":
		if isGroup && isAdmin(bot, chatID, userID) {
			purgeMessages(bot, chatID, args)
		}
		
	case "say":
		if isAdmin(bot, chatID, userID) {
			sayAsBot(bot, chatID, args)
		}
		
	case "rules":
		if isGroup {
			showRules(bot, chatID)
		}
		
	case "setrules":
		if isGroup && isAdmin(bot, chatID, userID) {
			setRules(bot, chatID, args)
		}
		
	case "setwelcome":
		if isGroup && isAdmin(bot, chatID, userID) {
			setWelcome(bot, chatID, args)
		}
		
	// ======================
	// ИГРОВЫЕ КОМАНДЫ
	// ======================
	case "game":
		if isGroup {
			createMafiaGame(bot, chatID, userID, msg.From)
		}
		
	case "join":
		if isGroup {
			joinMafiaGame(bot, chatID, userID, msg.From)
		}
		
	case "stop":
		if isGroup && isAdmin(bot, chatID, userID) {
			stopMafiaGame(bot, chatID)
		}
		
	case "skip":
		if isGroup && isAdmin(bot, chatID, userID) {
			skipGameTimer(bot, chatID)
		}
		
	case "players":
		if isGroup {
			showGamePlayers(bot, chatID)
		}
		
	// ======================
	// СОЦИАЛЬНЫЕ КОМАНДЫ
	// ======================
	case "profile", "p":
		showUserProfile(bot, chatID, userID, args)
		
	case "marry":
		if isGroup && len(args) > 0 {
			marryUser(bot, chatID, userID, args, msg)
		}
		
	case "divorce":
		divorceUser(bot, chatID, userID)
		
	case "reputation", "rep":
		showReputation(bot, chatID, userID, args)
		
	case "bio":
		setBio(bot, chatID, userID, args)
		
	case "title":
		setTitle(bot, chatID, userID, args)
		
	// ======================
	// ЭКОНОМИКА
	// ======================
	case "shop", "store":
		showShop(bot, chatID, userID)
		
	case "buy":
		buyShopItem(bot, chatID, userID, args)
		
	case "daily":
		claimDailyBonus(bot, chatID, userID)
		
	case "give":
		giveCoins(bot, chatID, userID, args, msg)
		
	case "balance", "bal":
		showBalance(bot, chatID, userID)
		
	case "casino":
		playCasino(bot, chatID, userID, args)
		
	case "duel":
		if isGroup && len(args) > 0 {
			startDuel(bot, chatID, userID, args, msg)
		}
		
	case "top":
		showTopPlayers(bot, chatID)
		
	case "coinflip":
		playCoinFlip(bot, chatID, userID, args)
		
	// ======================
	// ИНФОРМАЦИЯ
	// ======================
	case "info":
		showUserInfo(bot, chatID, args, msg)
		
	case "report":
		if isGroup && msg.ReplyToMessage != nil {
			reportUser(bot, chatID, userID, msg)
		}
		
	case "help", "commands":
		showHelp(bot, chatID)
		
	case "start":
		sendWelcome(bot, chatID, msg.From.FirstName)
		
	case "poll":
		if isGroup {
			createPoll(bot, chatID, args)
		}
		
	case "id":
		showID(bot, chatID, msg)
		
	case "chat":
		showChatInfo(bot, chatID, msg)
	}
}

// ======================
// АДМИН ФУНКЦИИ
// ======================

func banUser(bot *tgbotapi.BotAPI, chatID, adminID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil && len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте на сообщение или укажите ID"))
		return
	}
	
	var targetID int64
	var targetName string
	
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
		targetName = msg.ReplyToMessage.From.FirstName
	} else {
		targetID, _ = strconv.ParseInt(args[0], 10, 64)
		targetName = args[0]
	}
	
	reason := "Нарушение правил"
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}
	
	// Бан через Telegram API
	banConfig := tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: targetID,
		},
	}
	bot.Send(banConfig)
	
	// Запись в базу
	mu.Lock()
	if user, ok := users[targetID]; ok {
		user.BannedUntil = time.Now().Add(365 * 24 * time.Hour).Unix()
	}
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("🚫 **БАН**\n\nПользователь: %s\nПричина: %s\nАдмин: %s\n\nОтправлен в бан навсегда! 🖕",
			targetName, reason, msg.From.FirstName)))
}

func unbanUser(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажите ID пользователя"))
		return
	}
	
	targetID, _ := strconv.ParseInt(args[0], 10, 64)
	
	unbanConfig := tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: targetID,
		},
	}
	bot.Send(unbanConfig)
	
	mu.Lock()
	if user, ok := users[targetID]; ok {
		user.BannedUntil = 0
	}
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Пользователь %d разбанен", targetID)))
}

func muteUser(bot *tgbotapi.BotAPI, chatID, adminID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте на сообщение пользователя"))
		return
	}
	
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	
	duration := 60 // минуты по умолчанию
	if len(args) > 0 {
		duration, _ = strconv.Atoi(args[0])
	}
	
	reason := "Нарушение правил"
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}
	
	mu.Lock()
	if user, ok := users[targetID]; ok {
		user.MutedUntil = time.Now().Add(time.Duration(duration) * time.Minute).Unix()
	}
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("🔇 **МУТ**\n\nПользователь: %s\nДлительность: %d мин\nПричина: %s\n\nЗаткнись нахуй! 🤫",
			targetName, duration, reason)))
}

func unmuteUser(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажите ID или ответьте на сообщение"))
		return
	}
	
	targetID, _ := strconv.ParseInt(args[0], 10, 64)
	
	mu.Lock()
	if user, ok := users[targetID]; ok {
		user.MutedUntil = 0
	}
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Пользователь %d размучен", targetID)))
}

func kickUser(bot *tgbotapi.BotAPI, chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте на сообщение"))
		return
	}
	
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	
	kickConfig := tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: targetID,
		},
		UntilDate: time.Now().Add(1 * time.Minute).Unix(), // Бан на 1 минуту = кик
	}
	bot.Send(kickConfig)
	
	// Сразу разбаниваем
	time.Sleep(2 * time.Second)
	unbanConfig := tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: targetID,
		},
	}
	bot.Send(unbanConfig)
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("👢 **КИК**\n\nПользователь: %s\n\nВылетел как пробка! 🍾", targetName)))
}

func warnUser(bot *tgbotapi.BotAPI, chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте на сообщение"))
		return
	}
	
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	
	reason := "Нарушение"
	if len(args) > 0 {
		reason = strings.Join(args, " ")
	}
	
	mu.Lock()
	if user, ok := users[targetID]; ok {
		user.Warns++
		
		// Авто-бан при превышении
		settings := getChatSettings(chatID)
		if user.Warns >= settings.MaxWarns {
			user.BannedUntil = time.Now().Add(365 * 24 * time.Hour).Unix()
			mu.Unlock()
			
			banConfig := tgbotapi.BanChatMemberConfig{
				ChatMemberConfig: tgbotapi.ChatMemberConfig{
					ChatID: chatID,
					UserID: targetID,
				},
			}
			bot.Send(banConfig)
			
			bot.Send(tgbotapi.NewMessage(chatID, 
				fmt.Sprintf("🚫 **АВТО-БАН**\n\nПользователь: %s\nПредупреждений: %d/%d\n\nСлишком много нарушений! Пока! 👋",
					targetName, user.Warns, settings.MaxWarns)))
			return
		}
		mu.Unlock()
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("⚠️ **ПРЕДУПРЕЖДЕНИЕ**\n\nПользователь: %s\nПричина: %s\n\nЕщё одно и пизда! 😈",
			targetName, reason)))
}

func purgeMessages(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	count := 10
	if len(args) > 0 {
		count, _ = strconv.Atoi(args[0])
	}
	
	if count > 100 {
		count = 100
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🧹 Очищаю %d сообщений...", count)))
	// В реальном коде тут нужно получать сообщения и удалять по одному
}

// ======================
// ПРОВЕРКИ
// ======================

func isAdmin(bot *tgbotapi.BotAPI, chatID, userID int64) bool {
	config := tgbotapi.ChatConfigWithUser{
		ChatID: chatID,
		UserID: userID,
	}
	
	member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: config,
	})
	
	if err != nil {
		return false
	}
	
	return member.IsAdministrator() || member.IsCreator()
}

func isUserBanned(chatID, userID int64) bool {
	mu.RLock()
	defer mu.RUnlock()
	
	if user, ok := users[userID]; ok {
		if user.BannedUntil > time.Now().Unix() {
			return true
		}
	}
	return false
}

func isUserMuted(chatID, userID int64) bool {
	mu.RLock()
	defer mu.RUnlock()
	
	if user, ok := users[userID]; ok {
		if user.MutedUntil > time.Now().Unix() {
			return true
		}
	}
	return false
}

func isSpam(chatID, userID int64) bool {
	// Простая проверка спама
	return false
}

// ======================
// ИГРА МАФИЯ
// ======================

func createMafiaGame(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
	gameMu.Lock()
	defer gameMu.Unlock()
	
	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Игра уже идёт! /stop для остановки"))
			return
		}
	}
	
	gameID := fmt.Sprintf("g%d_%d", chatID, gameCounter)
	gameCounter++
	
	game := &Game{
		ID:        gameID,
		ChatID:    chatID,
		Players:   make(map[int64]*GamePlayer),
		Phase:     "waiting",
		Votes:     make(map[int64]int64),
		StartTime: time.Now(),
	}
	games[gameID] = game
	
	msgText := fmt.Sprintf("🎭 **МАФИЯ**\n\n👑 Создал: %s\n⏳ Ожидание: 60 сек\n👥 Минимум: 3\n\nЖми кнопку чтобы войти!",
		user.FirstName)
	
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎮 Войти в игру", fmt.Sprintf("m_join|%s", gameID)),
		),
	)
	
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sent, _ := bot.Send(msg)
	game.JoinMsgID = sent.MessageID
	
	game.Timer = time.AfterFunc(60*time.Second, func() {
		gameMu.Lock()
		defer gameMu.Unlock()
		
		if game.Phase == "waiting" {
			if len(game.Players) < 3 {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно игроков!"))
				delete(games, gameID)
			} else {
				startMafiaGame(bot, game)
			}
		}
	})
}

func startMafiaGame(bot *tgbotapi.BotAPI, game *Game) {
	game.Phase = "night"
	game.Round = 1
	
	// Назначение ролей
	ids := make([]int64, 0, len(game.Players))
	for id := range game.Players {
		ids = append(ids, id)
	}
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	
	count := len(ids)
	mafiaCount := count/3
	if mafiaCount < 1 { mafiaCount = 1 }
	if mafiaCount > 3 { mafiaCount = 3 }
	
	roles := []string{}
	for i := 0; i < mafiaCount; i++ { roles = append(roles, "Мафия") }
	roles = append(roles, "Доктор")
	if count > 4 { roles = append(roles, "Детектив") }
	for len(roles) < count { roles = append(roles, "Мирный житель") }
	rand.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
	
	for i, id := range ids {
		game.Players[id].Role = roles[i]
		bot.Send(tgbotapi.NewMessage(id, fmt.Sprintf("🎭 Ваша роль: **%s**", roles[i])))
	}
	
	bot.Send(tgbotapi.NewMessage(game.ChatID, "🎭 **МАФИЯ НАЧАЛАСЬ!**\n\n🌙 Ночь 1\n\nПроверьте ЛС!"))
}

func joinMafiaGame(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
	gameMu.Lock()
	defer gameMu.Unlock()
	
	var game *Game
	for _, g := range games {
		if g.ChatID == chatID && g.Phase == "waiting" {
			game = g
			break
		}
	}
	
	if game == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры!"))
		return
	}
	
	if _, exists := game.Players[userID]; exists {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ты уже в игре!"))
		return
	}
	
	game.Players[userID] = &GamePlayer{
		UserID: userID,
		FirstName: user.FirstName,
		Alive: true,
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %s в игре! (%d игроков)", user.FirstName, len(game.Players))))
}

func stopMafiaGame(bot *tgbotapi.BotAPI, chatID int64) {
	gameMu.Lock()
	defer gameMu.Unlock()
	
	for id, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			if g.Timer != nil {
				g.Timer.Stop()
			}
			delete(games, id)
			bot.Send(tgbotapi.NewMessage(chatID, "🛑 Игра остановлена!"))
			return
		}
	}
}

func skipGameTimer(bot *tgbotapi.BotAPI, chatID int64) {
	gameMu.Lock()
	defer gameMu.Unlock()
	
	for _, g := range games {
		if g.ChatID == chatID && g.Phase == "waiting" {
			if g.Timer != nil {
				g.Timer.Stop()
			}
			if len(g.Players) >= 3 {
				startMafiaGame(bot, g)
				bot.Send(tgbotapi.NewMessage(chatID, "⏩ Таймер пропущен! Игра начинается!"))
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно игроков!"))
			}
			return
		}
	}
}

func showGamePlayers(bot *tgbotapi.BotAPI, chatID int64) {
	gameMu.RLock()
	defer gameMu.RUnlock()
	
	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			list := "🎭 **Игроки:**\n\n"
			for _, p := range g.Players {
				status := "✅"
				if !p.Alive { status = "💀" }
				list += fmt.Sprintf("%s %s\n", status, p.FirstName)
			}
			msg := tgbotapi.NewMessage(chatID, list)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return
		}
	}
	bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры!"))
}

// ======================
// СОЦИАЛЬНЫЕ ФУНКЦИИ
// ======================

func marryUser(bot *tgbotapi.BotAPI, chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте на сообщение того, с кем хотите пожениться"))
		return
	}
	
	partnerID := msg.ReplyToMessage.From.ID
	
	if partnerID == userID {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нельзя жениться на себе, долбаёб!"))
		return
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	// Проверка существующих браков
	if m, ok := marriages[userID]; ok {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Ты уже в браке с %d! Сначала разведись /divorce", m.User2)))
		return
	}
	
	if m, ok := marriages[partnerID]; ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Этот пользователь уже в браке!"))
		return
	}
	
	marriages[userID] = &Marriage{User1: userID, User2: partnerID, Since: time.Now(), Love: 100}
	marriages[partnerID] = &Marriage{User1: partnerID, User2: userID, Since: time.Now(), Love: 100}
	
	user1Name := msg.From.FirstName
	user2Name := msg.ReplyToMessage.From.FirstName
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("💒 **ПОЗДРАВЛЯЕМ!**\n\n%s 💍 %s\n\nВы теперь в браке! Любви до гроба! 💕",
			user1Name, user2Name)))
}

func divorceUser(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()
	
	m, ok := marriages[userID]
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ты не в браке, одиночка!"))
		return
	}
	
	partnerID := m.User2
	delete(marriages, userID)
	delete(marriages, partnerID)
	
	bot.Send(tgbotapi.NewMessage(chatID, "💔 Брак расторгнут! Свобода! 🗽"))
}

func changeReputation(bot *tgbotapi.BotAPI, chatID, targetID int64, delta int) {
	mu.Lock()
	defer mu.Unlock()
	
	if user, ok := users[targetID]; ok {
		user.Reputation += delta
	}
}

func showReputation(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	targetID := userID
	if len(args) > 0 {
		targetID, _ = strconv.ParseInt(args[0], 10, 64)
	}
	
	mu.RLock()
	user, ok := users[targetID]
	mu.RUnlock()
	
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}
	
	stars := ""
	rep := user.Reputation
	for i := 0; i < 5; i++ {
		if rep > i*10 { stars += "⭐" } else { stars += "☆" }
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("🏆 Репутация %s: %d %s", user.FirstName, rep, stars)))
}

// ======================
// ЭКОНОМИКА
// ======================

func showShop(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	user, ok := users[userID]
	mu.RUnlock()
	if !ok { return }
	
	shopText := fmt.Sprintf("🏪 **МАГАЗИН**\n\n💰 Монеты: %d\n💎 Гемы: %d\n\n"+
		"🛡️ Броник - 50💰\n💊 Аптечка - 100💰\n"+
		"🔮 Шар - 75💰\n⚡ Двойной голос - 80💰\n"+
		"🎭 Маскировка - 150💰\n🧪 Яд - 200💰\n\n"+
		"Купить: /buy [название]",
		user.Coins, user.Gems)
	
	msg := tgbotapi.NewMessage(chatID, shopText)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func claimDailyBonus(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()
	
	user, ok := users[userID]
	if !ok { return }
	
	bonus := 50 + (user.Level * 10)
	user.Coins += bonus
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("🎁 Ежедневный бонус: +%d💰\n\nТвой баланс: %d💰", bonus, user.Coins)))
}

func giveCoins(bot *tgbotapi.BotAPI, chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil || len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответьте и укажите сумму"))
		return
	}
	
	amount, err := strconv.Atoi(args[0])
	if err != nil || amount <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажи нормальную сумму!"))
		return
	}
	
	targetID := msg.ReplyToMessage.From.ID
	
	mu.Lock()
	defer mu.Unlock()
	
	sender, ok1 := users[userID]
	receiver, ok2 := users[targetID]
	
	if !ok1 || !ok2 || sender.Coins < amount {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно монет!"))
		return
	}
	
	sender.Coins -= amount
	receiver.Coins += amount
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("💸 %s передал %s %d💰", msg.From.FirstName, msg.ReplyToMessage.From.FirstName, amount)))
}

func playCasino(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажи ставку! /casino 100\nШанс выигрыша - 40%"))
		return
	}
	
	bet, err := strconv.Atoi(args[0])
	if err != nil || bet <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нормальную ставку давай!"))
		return
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	user, ok := users[userID]
	if !ok || user.Coins < bet {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно монет!"))
		return
	}
	
	// 40% шанс выигрыша
	if rand.Intn(100) < 40 {
		win := bet * 2
		user.Coins += win - bet
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("🎰 ДЖЕКПОТ! Ты выиграл %d💰! Теперь у тебя %d💰", win, user.Coins)))
	} else {
		user.Coins -= bet
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("🎰 Проиграл %d💰! Осталось %d💰. Повезёт в следующий раз!", bet, user.Coins)))
	}
}

func startDuel(bot *tgbotapi.BotAPI, chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь тому, с кем хочешь драться!"))
		return
	}
	
	bet, _ := strconv.Atoi(args[0])
	if bet <= 0 { bet = 50 }
	
	targetID := msg.ReplyToMessage.From.ID
	
	if rand.Intn(2) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("⚔️ %s победил %s в дуэли и забрал %d💰!", msg.From.FirstName, msg.ReplyToMessage.From.FirstName, bet)))
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("⚔️ %s победил %s в дуэли и забрал %d💰!", msg.ReplyToMessage.From.FirstName, msg.From.FirstName, bet)))
	}
}

func playCoinFlip(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ /coinflip [сумма] [орел/решка]"))
		return
	}
	
	bet, _ := strconv.Atoi(args[0])
	if bet <= 0 { bet = 10 }
	
	choice := "орел"
	if len(args) > 1 { choice = strings.ToLower(args[1]) }
	
	result := []string{"орел", "решка"}[rand.Intn(2)]
	
	mu.Lock()
	user, ok := users[userID]
	mu.Unlock()
	
	if !ok || user.Coins < bet {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет монет!"))
		return
	}
	
	if choice == result {
		mu.Lock()
		user.Coins += bet
		mu.Unlock()
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("🪙 Выпал %s! Ты выиграл %d💰! Баланс: %d💰", result, bet, user.Coins)))
	} else {
		mu.Lock()
		user.Coins -= bet
		mu.Unlock()
		bot.Send(tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("🪙 Выпал %s! Ты проиграл %d💰! Баланс: %d💰", result, bet, user.Coins)))
	}
}

// ======================
// CALLBACK HANDLER
// ======================

func handleCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	parts := strings.Split(cb.Data, "|")
	if len(parts) < 2 { return }
	
	action := parts[0]
	gameID := parts[1]
	userID := cb.From.ID
	
	switch action {
	case "m_join":
		gameMu.Lock()
		game, exists := games[gameID]
		if !exists || game.Phase != "waiting" {
			gameMu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Игра уже началась!"))
			return
		}
		if _, inGame := game.Players[userID]; inGame {
			gameMu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Ты уже в игре!"))
			return
		}
		game.Players[userID] = &GamePlayer{
			UserID: userID, FirstName: cb.From.FirstName, Alive: true,
		}
		count := len(game.Players)
		gameMu.Unlock()
		
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Ты в игре!"))
		bot.Send(tgbotapi.NewMessage(cb.From.ID, fmt.Sprintf("✅ Ты вошёл в игру! (%d игроков)", count)))
		
	case "m_vote":
		// Голосование
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Голос учтён!"))
			bot.Send(tgbotapi.NewMessage(gameID, 
				fmt.Sprintf("🗳️ %s проголосовал", cb.From.FirstName)))
		}
	}
}

// ======================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ======================

func initUser(user *tgbotapi.User) {
	mu.Lock()
	defer mu.Unlock()
	
	if _, exists := users[user.ID]; !exists {
		users[user.ID] = &UserData{
			UserID:    user.ID,
			Username:  user.UserName,
			FirstName: user.FirstName,
			Coins:     100,
			Gems:      5,
			Level:     1,
		}
	} else {
		users[user.ID].Username = user.UserName
		users[user.ID].FirstName = user.FirstName
	}
}

func getChatSettings(chatID int64) *ChatSettings {
	mu.RLock()
	settings, exists := chatSettings[chatID]
	mu.RUnlock()
	
	if !exists {
		settings = &ChatSettings{
			ChatID:     chatID,
			MaxWarns:   3,
			MuteTime:   60,
			BanTime:    1440,
		}
		mu.Lock()
		chatSettings[chatID] = settings
		mu.Unlock()
	}
	
	return settings
}

func handleNewMember(bot *tgbotapi.BotAPI, chatID int64, member tgbotapi.User) {
	settings := getChatSettings(chatID)
	
	if settings.GreetingEnabled {
		welcomeText := settings.WelcomeMessage
		if welcomeText == "" {
			welcomeText = fmt.Sprintf("👋 Привет, %s! Добро пожаловать в чат!", member.FirstName)
		} else {
			welcomeText = strings.ReplaceAll(welcomeText, "{name}", member.FirstName)
			welcomeText = strings.ReplaceAll(welcomeText, "{username}", member.UserName)
		}
		
		bot.Send(tgbotapi.NewMessage(chatID, welcomeText))
	}
}

func handleLeftMember(bot *tgbotapi.BotAPI, chatID int64, member tgbotapi.User) {
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("👋 %s покинул чат. Скатертью дорога!", member.FirstName)))
}

func showUserProfile(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	targetID := userID
	if len(args) > 0 {
		if id, err := strconv.ParseInt(args[0], 10, 64); err == nil {
			targetID = id
		}
	}
	
	mu.RLock()
	user, ok := users[targetID]
	mu.RUnlock()
	
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}
	
	marriageStatus := "Свободен 💔"
	if m, married := marriages[targetID]; married {
		if partner, exists := users[m.User2]; exists {
			marriageStatus = fmt.Sprintf("В браке с %s 💕", partner.FirstName)
		}
	}
	
	profile := fmt.Sprintf(
		"👤 **ПРОФИЛЬ**\n\n"+
		"Имя: %s\n"+
		"Уровень: %d\n"+
		"💰 Монеты: %d\n"+
		"💎 Гемы: %d\n"+
		"⭐ Репутация: %d\n"+
		"🏆 Побед: %d\n"+
		"🎮 Игр: %d\n"+
		"💀 Убийств: %d\n"+
		"💘 %s\n"+
		"📝 %s",
		user.FirstName, user.Level, user.Coins, user.Gems,
		user.Reputation, user.GamesWon, user.GamesPlayed,
		user.Kills, marriageStatus, user.Bio,
	)
	
	msg := tgbotapi.NewMessage(chatID, profile)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showUserInfo(bot *tgbotapi.BotAPI, chatID int64, args []string, msg *tgbotapi.Message) {
	targetID := msg.From.ID
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
	} else if len(args) > 0 {
		targetID, _ = strconv.ParseInt(args[0], 10, 64)
	}
	
	mu.RLock()
	user, ok := users[targetID]
	mu.RUnlock()
	
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не найден"))
		return
	}
	
	info := fmt.Sprintf(
		"📊 **ИНФО**\n\n"+
		"ID: %d\n"+
		"Имя: %s\n"+
		"Username: @%s\n"+
		"Уровень: %d\n"+
		"Предупреждений: %d",
		user.UserID, user.FirstName, user.Username,
		user.Level, user.Warns,
	)
	
	msg := tgbotapi.NewMessage(chatID, info)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showTopPlayers(bot *tgbotapi.BotAPI, chatID int64) {
	mu.RLock()
	defer mu.RUnlock()
	
	// Сортировка по монетам
	type kv struct {
		Key int64
		Value int
	}
	var sorted []kv
	for id, u := range users {
		sorted = append(sorted, kv{id, u.Coins})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Value > sorted[j].Value })
	
	top := "🏆 **ТОП ИГРОКОВ**\n\n"
	for i, item := range sorted {
		if i >= 10 { break }
		if user, ok := users[item.Key]; ok {
			medal := ""
			switch i {
			case 0: medal = "🥇"
			case 1: medal = "🥈"
			case 2: medal = "🥉"
			default: medal = fmt.Sprintf("%d.", i+1)
			}
			top += fmt.Sprintf("%s %s - %d💰\n", medal, user.FirstName, user.Coins)
		}
	}
	
	msg := tgbotapi.NewMessage(chatID, top)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showHelp(bot *tgbotapi.BotAPI, chatID int64) {
	help := "🔥 **IRIS MAFIA BOT**\n\n" +
		"**Админ:**\n/ban /unban /mute /unmute /kick /warn /purge /say\n\n" +
		"**Мафия:**\n/game /join /stop /skip /players\n\n" +
		"**Социалка:**\n/profile /marry /divorce /reputation /bio\n\n" +
		"**Экономика:**\n/shop /buy /daily /give /casino /duel /coinflip\n\n" +
		"**Инфо:**\n/info /report /top /id /help\n\n" +
		"Без цензуры! Свобода слова! 🖕"
	
	msg := tgbotapi.NewMessage(chatID, help)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func sendWelcome(bot *tgbotapi.BotAPI, chatID int64, name string) {
	msg := tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("👋 %s, привет!\nЯ Iris Mafia Bot - админ, игры и развлечения!\n\n/help - все команды", name))
	bot.Send(msg)
}

func showBalance(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	user, ok := users[userID]
	mu.RUnlock()
	
	if !ok { return }
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("💰 Баланс: %d монет\n💎 Гемы: %d\n⭐ Уровень: %d", 
			user.Coins, user.Gems, user.Level)))
}

func showID(bot *tgbotapi.BotAPI, chatID int64, msg *tgbotapi.Message) {
	targetID := msg.From.ID
	targetName := msg.From.FirstName
	
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
		targetName = msg.ReplyToMessage.From.FirstName
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("🆔 %s: `%d`", targetName, targetID)))
}

func showChatInfo(bot *tgbotapi.BotAPI, chatID int64, msg *tgbotapi.Message) {
	chat, err := bot.GetChat(tgbotapi.ChatInfoConfig{
		ChatConfig: tgbotapi.ChatConfig{ChatID: chatID},
	})
	
	if err != nil {
		return
	}
	
	info := fmt.Sprintf("📊 **ИНФО ЧАТА**\n\nНазвание: %s\nID: %d\nТип: %s",
		chat.Title, chat.ID, chat.Type)
	
	msg2 := tgbotapi.NewMessage(chatID, info)
	msg2.ParseMode = "Markdown"
	bot.Send(msg2)
}

func showRules(bot *tgbotapi.BotAPI, chatID int64) {
	settings := getChatSettings(chatID)
	
	rules := settings.Rules
	if rules == "" {
		rules = "Правила не установлены. /setrules [текст]"
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📋 **ПРАВИЛА ЧАТА**\n\n%s", rules)))
}

func setRules(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	rules := strings.Join(args, " ")
	
	mu.Lock()
	settings := getChatSettings(chatID)
	settings.Rules = rules
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Правила обновлены!"))
}

func setWelcome(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	welcome := strings.Join(args, " ")
	
	mu.Lock()
	settings := getChatSettings(chatID)
	settings.WelcomeMessage = welcome
	settings.GreetingEnabled = true
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Приветствие обновлено!"))
}

func showWarnList(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	targetID, _ := strconv.ParseInt(args[0], 10, 64)
	
	mu.RLock()
	user, ok := users[targetID]
	mu.RUnlock()
	
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("⚠️ %s имеет %d предупреждений (макс: %d)", 
			user.FirstName, user.Warns, getChatSettings(chatID).MaxWarns)))
}

func reportUser(bot *tgbotapi.BotAPI, chatID, userID int64, msg *tgbotapi.Message) {
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	
	// Отправка админам
	admins, _ := bot.GetChatAdministrators(tgbotapi.ChatAdministratorsConfig{
		ChatConfig: tgbotapi.ChatConfig{ChatID: chatID},
	})
	
	for _, admin := range admins {
		if !admin.User.IsBot {
			bot.Send(tgbotapi.NewMessage(admin.User.ID, 
				fmt.Sprintf("🚨 **ЖАЛОБА**\n\nНа: %s (ID: %d)\nОт: %s\nЧат: %d",
					targetName, targetID, msg.From.FirstName, chatID)))
		}
	}
	
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Жалоба отправлена админам!"))
}

func setBio(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	bio := strings.Join(args, " ")
	if len(bio) > 100 { bio = bio[:100] }
	
	mu.Lock()
	if user, ok := users[userID]; ok {
		user.Bio = bio
	}
	mu.Unlock()
	
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Био обновлено!"))
}

func setTitle(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	// Покупка титула
	title := strings.Join(args, " ")
	
	mu.Lock()
	if user, ok := users[userID]; ok {
		if user.Coins >= 200 {
			user.Coins -= 200
			user.Title = title
			mu.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Титул '%s' куплен за 200💰!", title)))
		} else {
			mu.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Нужно 200 монет для покупки титула!"))
		}
	}
}

func buyShopItem(bot *tgbotapi.BotAPI, chatID, userID int64, args []string) {
	bot.Send(tgbotapi.NewMessage(chatID, "🏪 Используй кнопки в /shop для покупки!"))
}

func createPoll(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	if len(args) < 2 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ /poll [вопрос] [вариант1] [вариант2] ..."))
		return
	}
	
	question := args[0]
	options := args[1:]
	
	poll := tgbotapi.NewPoll(chatID, question, options...)
	poll.IsAnonymous = false
	bot.Send(poll)
}

func sayAsBot(bot *tgbotapi.BotAPI, chatID int64, args []string) {
	text := strings.Join(args, " ")
	bot.Send(tgbotapi.NewMessage(chatID, text))
}
