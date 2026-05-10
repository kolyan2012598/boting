package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const BOT_TOKEN = "8495592139:AAHJXCVAuQBpEbMfrcIbuGhP5CL8_zVQny4"

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
	Reputation  int    `json:"reputation"`
	Bio         string `json:"bio"`
	GamesWon    int    `json:"games_won"`
	GamesPlayed int    `json:"games_played"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
}

type ChatSettings struct {
	ChatID          int64  `json:"chat_id"`
	WelcomeMessage  string `json:"welcome_message"`
	Rules           string `json:"rules"`
	MaxWarns        int    `json:"max_warns"`
	GreetingEnabled bool   `json:"greeting_enabled"`
}

type Game struct {
	ID        string
	ChatID    int64
	Players   map[int64]*GamePlayer
	Phase     string
	Round     int
	Votes     map[int64]int64
	JoinMsgID int
	StartTime time.Time
	Timer     *time.Timer
}

type GamePlayer struct {
	UserID    int64
	FirstName string
	Role      string
	Alive     bool
	Defense   int
}

type Marriage struct {
	User1 int64
	User2 int64
	Love  int
}

var (
	users        = make(map[int64]*UserData)
	chatSettings = make(map[int64]*ChatSettings)
	games        = make(map[string]*Game)
	marriages    = make(map[int64]*Marriage)
	mu           sync.RWMutex
	gameMu       sync.RWMutex
	gameCounter  int
	bot          *tgbotapi.BotAPI
)

func main() {
	rand.Seed(time.Now().UnixNano())
	loadAllData()
	go autoSave()
	go startWebServer()

	var err error
	bot, err = tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}

	fmt.Printf("Бот запущен: @%s\n", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMessage(update.Message)
		}
		if update.CallbackQuery != nil {
			handleCallback(update.CallbackQuery)
		}
	}
}

func startWebServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Бот работает!")
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

func handleMessage(msg *tgbotapi.Message) {
	if msg.From.IsBot {
		return
	}

	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()

	initUser(msg.From)

	if isGroup {
		mu.RLock()
		user, exists := users[userID]
		mu.RUnlock()
		if exists {
			if user.BannedUntil > time.Now().Unix() {
				return
			}
			if user.MutedUntil > time.Now().Unix() {
				bot.Send(tgbotapi.NewDeleteMessage(chatID, msg.MessageID))
				return
			}
		}
	}

	if len(msg.NewChatMembers) > 0 {
		for _, member := range msg.NewChatMembers {
			handleNewMember(chatID, member)
		}
	}

	if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
		handleCommand(msg, isGroup)
	}
}

func handleCommand(msg *tgbotapi.Message, isGroup bool) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := strings.TrimPrefix(msg.Text, "/")
	text = strings.TrimPrefix(text, "!")

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	switch command {
	case "бан", "ban":
		if isGroup && isAdmin(chatID, userID) {
			banUser(chatID, args, msg)
		}
	case "мут", "mute":
		if isGroup && isAdmin(chatID, userID) {
			muteUser(chatID, args, msg)
		}
	case "размут", "unmute":
		if isGroup && isAdmin(chatID, userID) {
			unmuteUser(chatID, args)
		}
	case "кик", "kick":
		if isGroup && isAdmin(chatID, userID) {
			kickUser(chatID, msg)
		}
	case "варн", "пред", "warn":
		if isGroup && isAdmin(chatID, userID) {
			warnUser(chatID, args, msg)
		}
	case "игра", "game":
		if isGroup {
			createMafiaGame(chatID, userID, msg.From)
		}
	case "войти", "join":
		if isGroup {
			joinMafiaGame(chatID, userID, msg.From)
		}
	case "стоп", "stop":
		if isGroup && isAdmin(chatID, userID) {
			stopMafiaGame(chatID)
		}
	case "скип", "skip":
		if isGroup && isAdmin(chatID, userID) {
			skipGameTimer(chatID)
		}
	case "игроки", "players":
		showGamePlayers(chatID)
	case "профиль", "п", "profile", "p":
		showUserProfile(chatID, userID, args)
	case "брак", "жениться", "marry":
		if isGroup {
			marryUser(chatID, userID, msg)
		}
	case "развод", "divorce":
		divorceUser(chatID, userID)
	case "магазин", "shop":
		showShop(chatID, userID)
	case "бонус", "daily":
		claimDailyBonus(chatID, userID)
	case "передать", "give":
		giveCoins(chatID, userID, args, msg)
	case "баланс", "бал", "balance", "bal":
		showBalance(chatID, userID)
	case "казино", "casino":
		playCasino(chatID, userID, args)
	case "дуэль", "duel":
		if isGroup {
			startDuel(chatID, msg)
		}
	case "топ", "top":
		showTopPlayers(chatID)
	case "инфо", "info":
		showUserInfo(chatID, args, msg)
	case "жалоба", "репорт", "report":
		if msg.ReplyToMessage != nil {
			reportUser(chatID, msg)
		}
	case "помощь", "хелп", "help":
		showHelp(chatID)
	case "старт", "start":
		sendWelcome(chatID, msg.From.FirstName)
	case "правила", "rules":
		showRules(chatID)
	case "правила+", "setrules":
		if isAdmin(chatID, userID) {
			setRules(chatID, args)
		}
	case "приветствие", "setwelcome":
		if isAdmin(chatID, userID) {
			setWelcome(chatID, args)
		}
	case "айди", "id":
		showID(chatID, msg)
	case "опрос", "poll":
		createPoll(chatID, args)
	case "скажи", "say":
		if isAdmin(chatID, userID) {
			sayAsBot(chatID, args)
		}
	case "статус", "bio":
		setBio(chatID, userID, args)
	}
}

func isAdmin(chatID, userID int64) bool {
	member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID, UserID: userID,
		},
	})
	if err != nil {
		return false
	}
	return member.IsAdministrator() || member.IsCreator()
}

func initUser(user *tgbotapi.User) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := users[user.ID]; !exists {
		users[user.ID] = &UserData{
			UserID: user.ID, Username: user.UserName,
			FirstName: user.FirstName, Coins: 100, Gems: 5, Level: 1,
		}
	}
}

func getChatSettings(chatID int64) *ChatSettings {
	mu.RLock()
	s, exists := chatSettings[chatID]
	mu.RUnlock()
	if !exists {
		s = &ChatSettings{ChatID: chatID, MaxWarns: 3}
		mu.Lock()
		chatSettings[chatID] = s
		mu.Unlock()
	}
	return s
}

func banUser(chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil && len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение нарушителя или укажи ID"))
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
	bot.Send(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: targetID},
	})
	mu.Lock()
	if u, ok := users[targetID]; ok {
		u.BannedUntil = time.Now().Add(365 * 24 * time.Hour).Unix()
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %s забанен навсегда! Пока-пока!", targetName)))
}

func muteUser(chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение того, кого хочешь замутить"))
		return
	}
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	duration := 60
	if len(args) > 0 {
		duration, _ = strconv.Atoi(args[0])
	}
	mu.Lock()
	if u, ok := users[targetID]; ok {
		u.MutedUntil = time.Now().Add(time.Duration(duration) * time.Minute).Unix()
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔇 %s получил мут на %d минут. Заткнись!", targetName, duration)))
}

func unmuteUser(chatID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажи ID пользователя"))
		return
	}
	targetID, _ := strconv.ParseInt(args[0], 10, 64)
	mu.Lock()
	if u, ok := users[targetID]; ok {
		u.MutedUntil = 0
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Пользователь размучен. Можешь говорить!"))
}

func kickUser(chatID int64, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение кого кикаем"))
		return
	}
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	bot.Send(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: targetID},
		UntilDate:        time.Now().Add(1 * time.Minute).Unix(),
	})
	time.Sleep(2 * time.Second)
	bot.Send(tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: targetID},
	})
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("👢 %s вылетел из чата! Свобода!", targetName)))
}

func warnUser(chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение нарушителя"))
		return
	}
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	reason := "Нарушение правил"
	if len(args) > 0 {
		reason = strings.Join(args, " ")
	}
	mu.Lock()
	if u, ok := users[targetID]; ok {
		u.Warns++
		if u.Warns >= getChatSettings(chatID).MaxWarns {
			u.BannedUntil = time.Now().Add(365 * 24 * time.Hour).Unix()
			mu.Unlock()
			bot.Send(tgbotapi.BanChatMemberConfig{
				ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: targetID},
			})
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %s получил автобан! %d предупреждений - хватит!", targetName, u.Warns)))
			return
		}
		mu.Unlock()
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ %s получил предупреждение: %s. Ещё раз и бан!", targetName, reason)))
}

func createMafiaGame(chatID, userID int64, user *tgbotapi.User) {
	gameMu.Lock()
	defer gameMu.Unlock()
	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Игра уже идёт! /стоп чтобы остановить"))
			return
		}
	}
	gameID := fmt.Sprintf("g%d_%d", chatID, gameCounter)
	gameCounter++
	game := &Game{
		ID: gameID, ChatID: chatID,
		Players: make(map[int64]*GamePlayer), Phase: "waiting",
		Votes: make(map[int64]int64), StartTime: time.Now(),
	}
	games[gameID] = game

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎮 Войти в игру", fmt.Sprintf("m_join|%s", gameID)),
		),
	)
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🎭 **МАФИЯ**\n\n👑 Создал: %s\n⏳ Ожидание: 60 сек\n👥 Минимум: 3 игрока\n\nЖми кнопку чтобы войти!", user.FirstName))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sent, _ := bot.Send(msg)
	game.JoinMsgID = sent.MessageID

	game.Timer = time.AfterFunc(60*time.Second, func() {
		gameMu.Lock()
		defer gameMu.Unlock()
		if game.Phase == "waiting" {
			if len(game.Players) < 3 {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно игроков! Минимум 3."))
				delete(games, gameID)
			} else {
				startMafiaGame(game)
			}
		}
	})
}

func startMafiaGame(game *Game) {
	game.Phase = "night"
	game.Round = 1
	ids := make([]int64, 0, len(game.Players))
	for id := range game.Players {
		ids = append(ids, id)
	}
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	count := len(ids)
	mafiaCount := count / 3
	if mafiaCount < 1 {
		mafiaCount = 1
	}
	
	roles := []string{}
	for i := 0; i < mafiaCount; i++ {
		roles = append(roles, "Мафия")
	}
	roles = append(roles, "Доктор")
	if count > 4 {
		roles = append(roles, "Детектив")
	}
	for len(roles) < count {
		roles = append(roles, "Мирный житель")
	}
	rand.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
	
	for i, id := range ids {
		game.Players[id].Role = roles[i]
		bot.Send(tgbotapi.NewMessage(id, fmt.Sprintf("🎭 Твоя роль: **%s**\n\nУдачи в игре!", roles[i])))
	}
	
	playerList := ""
	for _, p := range game.Players {
		playerList += fmt.Sprintf("• %s\n", p.FirstName)
	}
	
	bot.Send(tgbotapi.NewMessage(game.ChatID, 
		fmt.Sprintf("🎭 **МАФИЯ НАЧАЛАСЬ!**\n\n🌙 Ночь 1\n\nИгроки:\n%s\nПроверьте ЛС - там ваша роль!", playerList)))
}

func joinMafiaGame(chatID, userID int64, user *tgbotapi.User) {
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
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры! Напиши /игра"))
		return
	}
	if _, exists := game.Players[userID]; exists {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ты уже в игре!"))
		return
	}
	game.Players[userID] = &GamePlayer{UserID: userID, FirstName: user.FirstName, Alive: true}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %s вошёл в игру! (%d игроков)", user.FirstName, len(game.Players))))
}

func stopMafiaGame(chatID int64) {
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
	bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры"))
}

func skipGameTimer(chatID int64) {
	gameMu.Lock()
	defer gameMu.Unlock()
	for _, g := range games {
		if g.ChatID == chatID && g.Phase == "waiting" {
			if g.Timer != nil {
				g.Timer.Stop()
			}
			if len(g.Players) >= 3 {
				startMafiaGame(g)
				bot.Send(tgbotapi.NewMessage(chatID, "⏩ Таймер пропущен! Игра начинается!"))
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно игроков!"))
			}
			return
		}
	}
}

func showGamePlayers(chatID int64) {
	gameMu.RLock()
	defer gameMu.RUnlock()
	for _, g := range games {
		if g.ChatID == chatID {
			list := "🎭 **Игроки:**\n\n"
			for _, p := range g.Players {
				list += fmt.Sprintf("• %s\n", p.FirstName)
			}
			msg := tgbotapi.NewMessage(chatID, list)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return
		}
	}
	bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры"))
}

func showUserProfile(chatID, userID int64, args []string) {
	targetID := userID
	if len(args) > 0 {
		targetID, _ = strconv.ParseInt(args[0], 10, 64)
	}
	mu.RLock()
	u, ok := users[targetID]
	mu.RUnlock()
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}
	text := fmt.Sprintf("👤 **ПРОФИЛЬ**\n\nИмя: %s\n💰 Монеты: %d\n💎 Гемы: %d\n⭐ Уровень: %d\n🏆 Побед: %d\n🎮 Игр: %d",
		u.FirstName, u.Coins, u.Gems, u.Level, u.GamesWon, u.GamesPlayed)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func marryUser(chatID, userID int64, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение того, с кем хочешь пожениться"))
		return
	}
	partnerID := msg.ReplyToMessage.From.ID
	mu.Lock()
	defer mu.Unlock()
	marriages[userID] = &Marriage{User1: userID, User2: partnerID, Love: 100}
	marriages[partnerID] = &Marriage{User1: partnerID, User2: userID, Love: 100}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💒 **ПОЗДРАВЛЯЕМ!**\n%s 💍 %s\n\nВы теперь в браке! Совет да любовь! 💕",
		msg.From.FirstName, msg.ReplyToMessage.From.FirstName)))
}

func divorceUser(chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()
	m, ok := marriages[userID]
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ты не в браке, одиночка!"))
		return
	}
	pid := m.User2
	delete(marriages, userID)
	delete(marriages, pid)
	bot.Send(tgbotapi.NewMessage(chatID, "💔 Брак расторгнут! Ты снова свободен!"))
}

func showShop(chatID, userID int64) {
	mu.RLock()
	u, ok := users[userID]
	mu.RUnlock()
	if !ok {
		return
	}
	text := fmt.Sprintf("🏪 **МАГАЗИН**\n\n💰 Твои монеты: %d\n\n🛡️ Бронежилет - 50💰\n💊 Аптечка - 100💰\n🔮 Хрустальный шар - 75💰\n\nКупить: /buy [название]", u.Coins)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func claimDailyBonus(chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()
	u, ok := users[userID]
	if !ok {
		return
	}
	bonus := 50 + u.Level*10
	u.Coins += bonus
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎁 Ежедневный бонус: +%d монет!\n💰 Баланс: %d монет", bonus, u.Coins)))
}

func giveCoins(chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil || len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь на сообщение и укажи сумму"))
		return
	}
	amount, _ := strconv.Atoi(args[0])
	if amount <= 0 {
		return
	}
	targetID := msg.ReplyToMessage.From.ID
	mu.Lock()
	defer mu.Unlock()
	sender := users[userID]
	receiver := users[targetID]
	if sender.Coins < amount {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ У тебя нет столько монет!"))
		return
	}
	sender.Coins -= amount
	receiver.Coins += amount
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💸 %s передал %s %d монет!", msg.From.FirstName, msg.ReplyToMessage.From.FirstName, amount)))
}

func showBalance(chatID, userID int64) {
	mu.RLock()
	u := users[userID]
	mu.RUnlock()
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💰 Монеты: %d | 💎 Гемы: %d | ⭐ Уровень: %d", u.Coins, u.Gems, u.Level)))
}

func playCasino(chatID, userID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "🎰 /казино [ставка]\nШанс выигрыша - 40%"))
		return
	}
	bet, _ := strconv.Atoi(args[0])
	mu.Lock()
	defer mu.Unlock()
	u := users[userID]
	if u.Coins < bet {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Недостаточно монет!"))
		return
	}
	if rand.Intn(100) < 40 {
		win := bet * 2
		u.Coins += win - bet
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎰 ДЖЕКПОТ! Ты выиграл %d монет! Баланс: %d", win, u.Coins)))
	} else {
		u.Coins -= bet
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎰 Проиграл %d монет... Баланс: %d", bet, u.Coins)))
	}
}

func startDuel(chatID int64, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ответь тому, с кем хочешь драться!"))
		return
	}
	p1 := msg.From.FirstName
	p2 := msg.ReplyToMessage.From.FirstName
	winner := p1
	if rand.Intn(2) == 0 {
		winner = p2
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚔️ **ДУЭЛЬ**\n\n%s vs %s\n\nПобедитель: %s! Слава победителю!", p1, p2, winner)))
}

func showTopPlayers(chatID int64) {
	mu.RLock()
	defer mu.RUnlock()
	var sorted []struct {
		Name  string
		Coins int
	}
	for _, u := range users {
		sorted = append(sorted, struct {
			Name  string
			Coins int
		}{u.FirstName, u.Coins})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Coins > sorted[j].Coins })
	text := "🏆 **ТОП ИГРОКОВ**\n\n"
	for i, s := range sorted {
		if i >= 10 {
			break
		}
		medal := ""
		switch i {
		case 0: medal = "🥇"
		case 1: medal = "🥈"
		case 2: medal = "🥉"
		default: medal = fmt.Sprintf("%d.", i+1)
		}
		text += fmt.Sprintf("%s %s - %d💰\n", medal, s.Name, s.Coins)
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showUserInfo(chatID int64, args []string, msg *tgbotapi.Message) {
	targetID := msg.From.ID
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
	} else if len(args) > 0 {
		targetID, _ = strconv.ParseInt(args[0], 10, 64)
	}
	mu.RLock()
	u := users[targetID]
	mu.RUnlock()
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📊 **ИНФОРМАЦИЯ**\n\nID: %d\nИмя: %s\nУровень: %d\nПредупреждений: %d\nПобед: %d",
		u.UserID, u.FirstName, u.Level, u.Warns, u.GamesWon)))
}

func reportUser(chatID int64, msg *tgbotapi.Message) {
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚨 Жалоба на %s отправлена администраторам!", msg.ReplyToMessage.From.FirstName)))
}

func showHelp(chatID int64) {
	text := "🔥 **IRIS MAFIA BOT**\n\n" +
		"**Админка:** /бан /мут /кик /варн\n" +
		"**Мафия:** /игра /войти /стоп /скип /игроки\n" +
		"**Социалка:** /профиль /брак /развод /статус\n" +
		"**Экономика:** /бонус /магазин /казино /передать /баланс\n" +
		"**Инфо:** /топ /инфо /жалоба /помощь /айди /правила\n\n" +
		"Без цензуры! Свобода слова! 🔥"
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func sendWelcome(chatID int64, name string) {
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("👋 Привет, %s!\nЯ Iris Mafia Bot - админ, игры и развлечения!\n\n/помощь - все команды", name)))
}

func showRules(chatID int64) {
	s := getChatSettings(chatID)
	r := s.Rules
	if r == "" {
		r = "📋 Правила не установлены. Админ: /правила+ [текст]"
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📋 **ПРАВИЛА ЧАТА**\n\n%s", r)))
}

func setRules(chatID int64, args []string) {
	mu.Lock()
	s := getChatSettings(chatID)
	s.Rules = strings.Join(args, " ")
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Правила обновлены!"))
}

func setWelcome(chatID int64, args []string) {
	mu.Lock()
	s := getChatSettings(chatID)
	s.WelcomeMessage = strings.Join(args, " ")
	s.GreetingEnabled = true
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Приветствие обновлено!"))
}

func showID(chatID int64, msg *tgbotapi.Message) {
	targetID := msg.From.ID
	name := msg.From.FirstName
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
		name = msg.ReplyToMessage.From.FirstName
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🆔 %s: `%d`", name, targetID)))
}

func createPoll(chatID int64, args []string) {
	if len(args) < 2 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ /опрос [вопрос] [вариант1] [вариант2] ..."))
		return
	}
	poll := tgbotapi.NewPoll(chatID, args[0], args[1:]...)
	poll.IsAnonymous = false
	bot.Send(poll)
}

func sayAsBot(chatID int64, args []string) {
	bot.Send(tgbotapi.NewMessage(chatID, strings.Join(args, " ")))
}

func setBio(chatID, userID int64, args []string) {
	bio := strings.Join(args, " ")
	if len(bio) > 100 {
		bio = bio[:100]
	}
	mu.Lock()
	if u, ok := users[userID]; ok {
		u.Bio = bio
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Статус обновлён!"))
}

func handleNewMember(chatID int64, member tgbotapi.User) {
	s := getChatSettings(chatID)
	if s.GreetingEnabled {
		t := s.WelcomeMessage
		if t == "" {
			t = fmt.Sprintf("👋 Привет, %s! Добро пожаловать в чат!", member.FirstName)
		}
		bot.Send(tgbotapi.NewMessage(chatID, t))
	}
}

func handleCallback(cb *tgbotapi.CallbackQuery) {
	parts := strings.Split(cb.Data, "|")
	if len(parts) < 2 {
		return
	}
	if parts[0] == "m_join" {
		gameID := parts[1]
		userID := cb.From.ID
		gameMu.Lock()
		game, exists := games[gameID]
		if !exists || game.Phase != "waiting" {
			gameMu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Игра уже началась!"))
			return
		}
		if _, in := game.Players[userID]; in {
			gameMu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "⚠️ Ты уже в игре!"))
			return
		}
		game.Players[userID] = &GamePlayer{UserID: userID, FirstName: cb.From.FirstName, Alive: true}
		c := len(game.Players)
		gameMu.Unlock()
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Ты в игре!"))
		bot.Send(tgbotapi.NewMessage(cb.From.ID, fmt.Sprintf("✅ Ты вошёл в игру! (%d игроков)\nЖди начала!", c)))
	}
}
