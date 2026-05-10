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

	fmt.Printf("Bot started: @%s\n", bot.Self.UserName)

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
		fmt.Fprintf(w, "Bot Online")
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

	// ИСПРАВЛЕНО: NewChatMembers - слайс, не указатель
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
	case "ban":
		if isGroup && isAdmin(chatID, userID) {
			banUser(chatID, args, msg)
		}
	case "mute":
		if isGroup && isAdmin(chatID, userID) {
			muteUser(chatID, args, msg)
		}
	case "unmute":
		if isGroup && isAdmin(chatID, userID) {
			unmuteUser(chatID, args)
		}
	case "kick":
		if isGroup && isAdmin(chatID, userID) {
			kickUser(chatID, msg)
		}
	case "warn":
		if isGroup && isAdmin(chatID, userID) {
			warnUser(chatID, args, msg)
		}
	case "game":
		if isGroup {
			createMafiaGame(chatID, userID, msg.From)
		}
	case "join":
		if isGroup {
			joinMafiaGame(chatID, userID, msg.From)
		}
	case "stop":
		if isGroup && isAdmin(chatID, userID) {
			stopMafiaGame(chatID)
		}
	case "skip":
		if isGroup && isAdmin(chatID, userID) {
			skipGameTimer(chatID)
		}
	case "players":
		showGamePlayers(chatID)
	case "profile", "p":
		showUserProfile(chatID, userID, args)
	case "marry":
		if isGroup {
			marryUser(chatID, userID, msg)
		}
	case "divorce":
		divorceUser(chatID, userID)
	case "shop":
		showShop(chatID, userID)
	case "daily":
		claimDailyBonus(chatID, userID)
	case "give":
		giveCoins(chatID, userID, args, msg)
	case "balance", "bal":
		showBalance(chatID, userID)
	case "casino":
		playCasino(chatID, userID, args)
	case "duel":
		if isGroup {
			startDuel(chatID, userID, args, msg)
		}
	case "top":
		showTopPlayers(chatID)
	case "info":
		showUserInfo(chatID, args, msg)
	case "report":
		if msg.ReplyToMessage != nil {
			reportUser(chatID, msg)
		}
	case "help":
		showHelp(chatID)
	case "start":
		sendWelcome(chatID, msg.From.FirstName)
	case "rules":
		showRules(chatID)
	case "setrules":
		if isAdmin(chatID, userID) {
			setRules(chatID, args)
		}
	case "setwelcome":
		if isAdmin(chatID, userID) {
			setWelcome(chatID, args)
		}
	case "id":
		showID(chatID, msg)
	case "poll":
		createPoll(chatID, args)
	case "say":
		if isAdmin(chatID, userID) {
			sayAsBot(chatID, args)
		}
	case "bio":
		setBio(chatID, userID, args)
	}
}

func isAdmin(chatID, userID int64) bool {
	member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
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
		bot.Send(tgbotapi.NewMessage(chatID, "Reply or specify ID"))
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
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %s banned!", targetName)))
}

func muteUser(chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply to message"))
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
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔇 %s muted %d min", targetName, duration)))
}

func unmuteUser(chatID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Specify ID"))
		return
	}
	targetID, _ := strconv.ParseInt(args[0], 10, 64)
	mu.Lock()
	if u, ok := users[targetID]; ok {
		u.MutedUntil = 0
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Unmuted"))
}

func kickUser(chatID int64, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply to message"))
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
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("👢 %s kicked!", targetName)))
}

func warnUser(chatID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply to message"))
		return
	}
	targetID := msg.ReplyToMessage.From.ID
	targetName := msg.ReplyToMessage.From.FirstName
	reason := "Violation"
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
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %s auto-banned! (%d warns)", targetName, u.Warns)))
			return
		}
		mu.Unlock()
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ %s warned: %s", targetName, reason)))
}

func createMafiaGame(chatID, userID int64, user *tgbotapi.User) {
	gameMu.Lock()
	defer gameMu.Unlock()
	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			bot.Send(tgbotapi.NewMessage(chatID, "Game exists! /stop"))
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
			tgbotapi.NewInlineKeyboardButtonData("🎮 Join", fmt.Sprintf("m_join|%s", gameID)),
		),
	)
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🎭 MAFIA by %s\n60s | Min 3", user.FirstName))
	msg.ReplyMarkup = keyboard
	sent, _ := bot.Send(msg)
	game.JoinMsgID = sent.MessageID

	game.Timer = time.AfterFunc(60*time.Second, func() {
		gameMu.Lock()
		defer gameMu.Unlock()
		if game.Phase == "waiting" {
			if len(game.Players) < 3 {
				bot.Send(tgbotapi.NewMessage(chatID, "Not enough players!"))
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
		roles = append(roles, "Mafia")
	}
	roles = append(roles, "Doctor")
	if count > 4 {
		roles = append(roles, "Detective")
	}
	for len(roles) < count {
		roles = append(roles, "Citizen")
	}
	rand.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
	for i, id := range ids {
		game.Players[id].Role = roles[i]
		bot.Send(tgbotapi.NewMessage(id, fmt.Sprintf("Your role: %s", roles[i])))
	}
	bot.Send(tgbotapi.NewMessage(game.ChatID, "🎭 GAME STARTED! Check DM!"))
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
		bot.Send(tgbotapi.NewMessage(chatID, "No active game!"))
		return
	}
	if _, exists := game.Players[userID]; exists {
		bot.Send(tgbotapi.NewMessage(chatID, "Already in!"))
		return
	}
	game.Players[userID] = &GamePlayer{UserID: userID, FirstName: user.FirstName, Alive: true}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %s joined! (%d)", user.FirstName, len(game.Players))))
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
			bot.Send(tgbotapi.NewMessage(chatID, "🛑 Stopped!"))
			return
		}
	}
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
				bot.Send(tgbotapi.NewMessage(chatID, "⏩ Started!"))
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
			list := "Players:\n"
			for _, p := range g.Players {
				list += fmt.Sprintf("%s\n", p.FirstName)
			}
			bot.Send(tgbotapi.NewMessage(chatID, list))
			return
		}
	}
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
		bot.Send(tgbotapi.NewMessage(chatID, "Not found"))
		return
	}
	text := fmt.Sprintf("👤 %s\n💰 %d coins\n💎 %d gems\n⭐ Lv %d\n🏆 %d wins",
		u.FirstName, u.Coins, u.Gems, u.Level, u.GamesWon)
	bot.Send(tgbotapi.NewMessage(chatID, text))
}

func marryUser(chatID, userID int64, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply to someone"))
		return
	}
	partnerID := msg.ReplyToMessage.From.ID
	mu.Lock()
	defer mu.Unlock()
	marriages[userID] = &Marriage{User1: userID, User2: partnerID, Love: 100}
	marriages[partnerID] = &Marriage{User1: partnerID, User2: userID, Love: 100}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💒 %s + %s = ❤️", msg.From.FirstName, msg.ReplyToMessage.From.FirstName)))
}

func divorceUser(chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()
	m, ok := marriages[userID]
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "Not married!"))
		return
	}
	pid := m.User2
	delete(marriages, userID)
	delete(marriages, pid)
	bot.Send(tgbotapi.NewMessage(chatID, "💔 Divorced!"))
}

func showShop(chatID, userID int64) {
	mu.RLock()
	u, ok := users[userID]
	mu.RUnlock()
	if !ok {
		return
	}
	text := fmt.Sprintf("🏪 Shop\n💰 %d coins\n\n🛡️ Armor - 50💰\n💊 Medkit - 100💰\n🔮 Crystal - 75💰", u.Coins)
	bot.Send(tgbotapi.NewMessage(chatID, text))
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
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎁 +%d coins! Bal: %d", bonus, u.Coins)))
}

func giveCoins(chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil || len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply + amount"))
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
		bot.Send(tgbotapi.NewMessage(chatID, "Not enough!"))
		return
	}
	sender.Coins -= amount
	receiver.Coins += amount
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💸 %d coins sent!", amount)))
}

func showBalance(chatID, userID int64) {
	mu.RLock()
	u := users[userID]
	mu.RUnlock()
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("💰 %d | 💎 %d", u.Coins, u.Gems)))
}

func playCasino(chatID, userID int64, args []string) {
	if len(args) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "/casino [bet]"))
		return
	}
	bet, _ := strconv.Atoi(args[0])
	mu.Lock()
	defer mu.Unlock()
	u := users[userID]
	if u.Coins < bet {
		bot.Send(tgbotapi.NewMessage(chatID, "Not enough!"))
		return
	}
	if rand.Intn(100) < 40 {
		u.Coins += bet
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎰 WIN +%d!", bet)))
	} else {
		u.Coins -= bet
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎰 LOSE -%d!", bet)))
	}
}

func startDuel(chatID, userID int64, args []string, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Reply to someone"))
		return
	}
	p1 := msg.From.FirstName
	p2 := msg.ReplyToMessage.From.FirstName
	winner := p1
	if rand.Intn(2) == 0 {
		winner = p2
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚔️ %s won vs %s!", winner, p2)))
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
	text := "🏆 TOP:\n"
	for i, s := range sorted {
		if i >= 10 {
			break
		}
		text += fmt.Sprintf("%d. %s - %d💰\n", i+1, s.Name, s.Coins)
	}
	bot.Send(tgbotapi.NewMessage(chatID, text))
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
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ID: %d\nName: %s\nLv: %d\nWarns: %d",
		u.UserID, u.FirstName, u.Level, u.Warns)))
}

func reportUser(chatID int64, msg *tgbotapi.Message) {
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚨 Reported %s!", msg.ReplyToMessage.From.FirstName)))
}

func showHelp(chatID int64) {
	text := "🔥 IRIS MAFIA BOT\n\nAdmin: /ban /mute /kick /warn\nGame: /game /join /stop\nFun: /profile /marry /shop /daily /casino /top"
	bot.Send(tgbotapi.NewMessage(chatID, text))
}

func sendWelcome(chatID int64, name string) {
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("👋 %s!", name)))
}

func showRules(chatID int64) {
	s := getChatSettings(chatID)
	r := s.Rules
	if r == "" {
		r = "No rules"
	}
	bot.Send(tgbotapi.NewMessage(chatID, r))
}

func setRules(chatID int64, args []string) {
	mu.Lock()
	s := getChatSettings(chatID)
	s.Rules = strings.Join(args, " ")
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Updated!"))
}

func setWelcome(chatID int64, args []string) {
	mu.Lock()
	s := getChatSettings(chatID)
	s.WelcomeMessage = strings.Join(args, " ")
	s.GreetingEnabled = true
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Updated!"))
}

func showID(chatID int64, msg *tgbotapi.Message) {
	targetID := msg.From.ID
	name := msg.From.FirstName
	if msg.ReplyToMessage != nil {
		targetID = msg.ReplyToMessage.From.ID
		name = msg.ReplyToMessage.From.FirstName
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🆔 %s: %d", name, targetID)))
}

func createPoll(chatID int64, args []string) {
	if len(args) < 2 {
		return
	}
	bot.Send(tgbotapi.NewPoll(chatID, args[0], args[1:]...))
}

func sayAsBot(chatID int64, args []string) {
	bot.Send(tgbotapi.NewMessage(chatID, strings.Join(args, " ")))
}

func setBio(chatID, userID int64, args []string) {
	mu.Lock()
	if u, ok := users[userID]; ok {
		u.Bio = strings.Join(args, " ")
	}
	mu.Unlock()
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Bio updated!"))
}

func handleNewMember(chatID int64, member tgbotapi.User) {
	s := getChatSettings(chatID)
	if s.GreetingEnabled {
		t := s.WelcomeMessage
		if t == "" {
			t = fmt.Sprintf("Welcome %s!", member.FirstName)
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
			bot.Request(tgbotapi.NewCallback(cb.ID, "Game started!"))
			return
		}
		if _, in := game.Players[userID]; in {
			gameMu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Already in!"))
			return
		}
		game.Players[userID] = &GamePlayer{UserID: userID, FirstName: cb.From.FirstName, Alive: true}
		c := len(game.Players)
		gameMu.Unlock()
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Joined!"))
		bot.Send(tgbotapi.NewMessage(cb.From.ID, fmt.Sprintf("You joined! (%d)", c)))
	}
}
