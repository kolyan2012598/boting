package main

import (
	"fmt"
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

type Player struct {
	UserID    int64
	Username  string
	FirstName string
	Role      string
	Alive     bool
	Coins     int
	Defense   int
	VotedFor  int64
}

type Game struct {
	ID          string
	ChatID      int64
	Players     map[int64]*Player
	Phase       string
	Round       int
	MinPlayers  int
	Votes       map[int64]int64
	MafiaTarget int64
	DoctorSave  int64
	JoinMsgID   int
	StartTime   time.Time
	Timer       *time.Timer
}

var (
	games       = make(map[string]*Game)
	playersData = make(map[int64]*Player)
	mu          sync.RWMutex
	gameCounter int
)

func main() {
	rand.Seed(time.Now().UnixNano())

	// Web server for Render
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Mafia Bot is running!")
		})
		port := os.Getenv("PORT")
		if port == "" {
			port = "10000"
		}
		http.ListenAndServe(":"+port, nil)
	}()

	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}

	fmt.Println("Bot started:", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMsg(bot, update.Message)
		}
		if update.CallbackQuery != nil {
			handleCb(bot, update.CallbackQuery)
		}
	}
}

func handleMsg(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()

	mu.Lock()
	if _, exists := playersData[userID]; !exists {
		playersData[userID] = &Player{
			UserID: userID, Username: msg.From.UserName,
			FirstName: msg.From.FirstName, Coins: 50,
		}
	}
	mu.Unlock()

	switch {
	case text == "/game" && isGroup:
		createGame(bot, chatID, userID, msg.From)
	case text == "/join" && isGroup:
		joinGameCmd(bot, chatID, userID, msg.From)
	case text == "/stop" && isGroup:
		stopGame(bot, chatID, userID)
	case text == "/skip" && isGroup:
		skipTimer(bot, chatID, userID)
	case text == "/profile":
		showProfile(bot, chatID, userID)
	case text == "/shop":
		showShop(bot, chatID, userID)
	case text == "/help":
		showHelp(bot, chatID)
	}
}

func handleCb(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	parts := strings.Split(cb.Data, "|")
	if len(parts) < 2 {
		return
	}
	action := parts[0]
	gameID := parts[1]
	userID := cb.From.ID

	if action == "join" {
		mu.Lock()
		game, exists := games[gameID]
		if !exists || game.Phase != "waiting" {
			mu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Game not found or started"))
			return
		}
		if _, inGame := game.Players[userID]; inGame {
			mu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Already in game!"))
			return
		}
		user := cb.From
		game.Players[userID] = &Player{
			UserID: userID, Username: user.UserName,
			FirstName: user.FirstName, Alive: true,
		}
		count := len(game.Players)
		msgID := game.JoinMsgID
		chatID := game.ChatID
		mu.Unlock()

		bot.Send(tgbotapi.NewMessage(userID, fmt.Sprintf("You joined! Players: %d", count)))

		updateText := fmt.Sprintf("MAFIA\n\nWaiting...\nPlayers: %d\n\nPress button to join!", count)
		edit := tgbotapi.NewEditMessageText(chatID, msgID, updateText)
		edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
			InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Join Game", fmt.Sprintf("join|%s", gameID))},
			},
		}
		bot.Send(edit)
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("%s joined! (%d)", user.FirstName, count)))
		bot.Request(tgbotapi.NewCallback(cb.ID, "Joined!"))
		return
	}

	if action == "vote" && len(parts) >= 3 {
		targetID, _ := strconv.ParseInt(parts[2], 10, 64)
		mu.Lock()
		game, exists := games[gameID]
		if !exists || game.Phase != "day" {
			mu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Voting closed"))
			return
		}
		voter, ok := game.Players[userID]
		if !ok || !voter.Alive {
			mu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Cannot vote"))
			return
		}
		target, ok := game.Players[targetID]
		if !ok || !target.Alive {
			mu.Unlock()
			bot.Request(tgbotapi.NewCallback(cb.ID, "Target invalid"))
			return
		}
		voter.VotedFor = targetID
		game.Votes[userID] = targetID
		chatID := game.ChatID
		voterName := voter.FirstName
		targetName := target.FirstName
		mu.Unlock()

		bot.Request(tgbotapi.NewCallback(cb.ID, fmt.Sprintf("Voted for %s", targetName)))
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("%s voted for %s", voterName, targetName)))
		return
	}

	mu.RLock()
	game, exists := games[gameID]
	mu.RUnlock()
	if !exists || len(parts) < 3 {
		return
	}

	targetID, _ := strconv.ParseInt(parts[2], 10, 64)

	switch action {
	case "kill":
		mu.Lock()
		if game.Players[userID].Role == "Мафия" {
			game.MafiaTarget = targetID
			bot.Request(tgbotapi.NewCallback(cb.ID, "Target selected"))
		}
		mu.Unlock()

	case "save":
		mu.Lock()
		if game.Players[userID].Role == "Доктор" {
			game.DoctorSave = targetID
			bot.Request(tgbotapi.NewCallback(cb.ID, "Protected"))
		}
		mu.Unlock()

	case "check":
		mu.Lock()
		if game.Players[userID].Role == "Детектив" {
			target := game.Players[targetID]
			mu.Unlock()
			bot.Send(tgbotapi.NewMessage(userID, fmt.Sprintf("%s is %s", target.FirstName, target.Role)))
			bot.Request(tgbotapi.NewCallback(cb.ID, "Checked"))
		} else {
			mu.Unlock()
		}
	}
}

func createGame(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
	mu.Lock()
	defer mu.Unlock()

	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			bot.Send(tgbotapi.NewMessage(chatID, "Game already exists! /stop to cancel"))
			return
		}
	}

	gameID := fmt.Sprintf("g%d_%d", chatID, gameCounter)
	gameCounter++

	game := &Game{
		ID: gameID, ChatID: chatID,
		Players: make(map[int64]*Player),
		Phase: "waiting", Round: 0,
		MinPlayers: 3,
		Votes: make(map[int64]int64),
		StartTime: time.Now(),
	}
	games[gameID] = game

	msgText := fmt.Sprintf("MAFIA\n\n%s created game!\nTimer: 60 sec\nMin: 3 players\n\nPress button!", user.FirstName)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Join Game", fmt.Sprintf("join|%s", gameID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ReplyMarkup = keyboard
	sent, _ := bot.Send(msg)
	game.JoinMsgID = sent.MessageID

	game.Timer = time.AfterFunc(60*time.Second, func() {
		mu.Lock()
		defer mu.Unlock()
		if game.Phase == "waiting" {
			if len(game.Players) < game.MinPlayers {
				bot.Send(tgbotapi.NewMessage(chatID, "Not enough players!"))
				delete(games, gameID)
			} else {
				startGame(bot, game)
			}
		}
	})
}

func joinGameCmd(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
	mu.Lock()
	defer mu.Unlock()

	var game *Game
	for _, g := range games {
		if g.ChatID == chatID && g.Phase == "waiting" {
			game = g
			break
		}
	}
	if game == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "No active game! /game"))
		return
	}
	if _, exists := game.Players[userID]; exists {
		bot.Send(tgbotapi.NewMessage(chatID, "Already in game!"))
		return
	}

	game.Players[userID] = &Player{
		UserID: userID, Username: user.UserName,
		FirstName: user.FirstName, Alive: true,
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("%s joined! (%d)", user.FirstName, len(game.Players))))
}

func stopGame(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()

	for id, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			if g.Timer != nil {
				g.Timer.Stop()
			}
			g.Phase = "ended"
			delete(games, id)
			bot.Send(tgbotapi.NewMessage(chatID, "Game stopped!"))
			return
		}
	}
	bot.Send(tgbotapi.NewMessage(chatID, "No active game!"))
}

func skipTimer(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.Lock()
	defer mu.Unlock()

	for _, g := range games {
		if g.ChatID == chatID && g.Phase == "waiting" {
			if g.Timer != nil {
				g.Timer.Stop()
			}
			if len(g.Players) < g.MinPlayers {
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Need %d more!", g.MinPlayers-len(g.Players))))
				return
			}
			bot.Send(tgbotapi.NewMessage(chatID, "Timer skipped! Starting..."))
			startGame(bot, g)
			return
		}
	}
	bot.Send(tgbotapi.NewMessage(chatID, "No waiting game!"))
}

func startGame(bot *tgbotapi.BotAPI, game *Game) {
	game.Phase = "night"
	game.Round = 1
	assignRoles(game)

	for _, p := range game.Players {
		bot.Send(tgbotapi.NewMessage(p.UserID, fmt.Sprintf("Your role: %s\n\n%s", p.Role, getRoleDesc(p.Role))))
	}

	list := ""
	for _, p := range game.Players {
		list += fmt.Sprintf("%s\n", p.FirstName)
	}
	bot.Send(tgbotapi.NewMessage(game.ChatID, fmt.Sprintf("GAME STARTED!\n\nNIGHT %d\n\nPlayers:\n%s\nCheck DM!", game.Round, list)))

	nightPhase(bot, game)
}

func assignRoles(game *Game) {
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
	if mafiaCount > 3 {
		mafiaCount = 3
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
	}
}

func nightPhase(bot *tgbotapi.BotAPI, game *Game) {
	alive := getAlive(game)
	for _, p := range alive {
		var btns [][]tgbotapi.InlineKeyboardButton
		switch p.Role {
		case "Мафия":
			for _, t := range alive {
				if t.UserID != p.UserID && t.Role != "Мафия" {
					btns = append(btns, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("Kill %s", t.FirstName),
							fmt.Sprintf("kill|%s|%d", game.ID, t.UserID))))
				}
			}
		case "Доктор":
			for _, t := range alive {
				btns = append(btns, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("Save %s", t.FirstName),
						fmt.Sprintf("save|%s|%d", game.ID, t.UserID))))
			}
		case "Детектив":
			for _, t := range alive {
				if t.UserID != p.UserID {
					btns = append(btns, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("Check %s", t.FirstName),
							fmt.Sprintf("check|%s|%d", game.ID, t.UserID))))
				}
			}
		}
		if len(btns) > 0 {
			kb := tgbotapi.NewInlineKeyboardMarkup(btns...)
			msg := tgbotapi.NewMessage(p.UserID, fmt.Sprintf("Night %d | Role: %s\nChoose:", game.Round, p.Role))
			msg.ReplyMarkup = kb
			bot.Send(msg)
		}
	}

	game.Timer = time.AfterFunc(30*time.Second, func() {
		mu.Lock()
		defer mu.Unlock()
		if game.Phase == "night" {
			processNight(bot, game)
		}
	})
}

func processNight(bot *tgbotapi.BotAPI, game *Game) {
	victim := game.MafiaTarget
	if victim != 0 && game.DoctorSave != victim {
		p := game.Players[victim]
		if p.Defense > 0 {
			p.Defense--
			bot.Send(tgbotapi.NewMessage(game.ChatID, fmt.Sprintf("%s saved by armor!", p.FirstName)))
		} else {
			p.Alive = false
			bot.Send(tgbotapi.NewMessage(game.ChatID, fmt.Sprintf("%s was killed!", p.FirstName)))
		}
	} else if victim != 0 {
		bot.Send(tgbotapi.NewMessage(game.ChatID, "Someone was saved by doctor!"))
	}

	if checkWin(bot, game) {
		return
	}

	game.Phase = "day"
	game.Votes = make(map[int64]int64)
	game.MafiaTarget = 0
	game.DoctorSave = 0
	for _, p := range game.Players {
		p.VotedFor = 0
	}
	dayPhase(bot, game)
}

func dayPhase(bot *tgbotapi.BotAPI, game *Game) {
	alive := getAlive(game)
	list := ""
	for _, p := range alive {
		list += fmt.Sprintf("%s\n", p.FirstName)
	}

	bot.Send(tgbotapi.NewMessage(game.ChatID, fmt.Sprintf("DAY %d\n\nAlive:\n%s\nVote in DM!", game.Round, list)))

	for _, p := range alive {
		var btns [][]tgbotapi.InlineKeyboardButton
		for _, t := range alive {
			if t.UserID != p.UserID {
				btns = append(btns, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("Vote %s", t.FirstName),
						fmt.Sprintf("vote|%s|%d", game.ID, t.UserID))))
			}
		}
		if len(btns) > 0 {
			kb := tgbotapi.NewInlineKeyboardMarkup(btns...)
			msg := tgbotapi.NewMessage(p.UserID, fmt.Sprintf("Day %d - Vote:", game.Round))
			msg.ReplyMarkup = kb
			bot.Send(msg)
		}
	}

	game.Timer = time.AfterFunc(45*time.Second, func() {
		mu.Lock()
		defer mu.Unlock()
		if game.Phase == "day" {
			processVoting(bot, game)
		}
	})
}

func processVoting(bot *tgbotapi.BotAPI, game *Game) {
	voteCount := make(map[int64]int)
	for _, target := range game.Votes {
		if target != 0 {
			voteCount[target]++
		}
	}

	maxVotes := 0
	var eliminated int64
	for id, count := range voteCount {
		if count > maxVotes {
			maxVotes = count
			eliminated = id
		}
	}

	if eliminated != 0 {
		p := game.Players[eliminated]
		p.Alive = false
		bot.Send(tgbotapi.NewMessage(game.ChatID, fmt.Sprintf("%s eliminated! Role: %s", p.FirstName, p.Role)))
	} else {
		bot.Send(tgbotapi.NewMessage(game.ChatID, "No one eliminated"))
	}

	if checkWin(bot, game) {
		return
	}

	game.Round++
	game.Votes = make(map[int64]int64)
	game.Phase = "night"
	nightPhase(bot, game)
}

func checkWin(bot *tgbotapi.BotAPI, game *Game) bool {
	alive := getAlive(game)
	var mafia, citizens int
	for _, p := range alive {
		if p.Role == "Мафия" {
			mafia++
		} else {
			citizens++
		}
	}

	if mafia >= citizens {
		endGame(bot, game, "mafia")
		return true
	}
	if mafia == 0 {
		endGame(bot, game, "citizens")
		return true
	}
	return false
}

func endGame(bot *tgbotapi.BotAPI, game *Game, winner string) {
	if game.Timer != nil {
		game.Timer.Stop()
	}
	game.Phase = "ended"

	text := ""
	if winner == "citizens" {
		text = "CITIZENS WIN!"
	}
	if winner == "mafia" {
		text = "MAFIA WINS!"
	}

	text += "\n\nResults:\n"
	for _, p := range game.Players {
		status := "DEAD"
		if p.Alive {
			status = "ALIVE"
		}
		text += fmt.Sprintf("%s %s - %s\n", status, p.FirstName, p.Role)

		if (winner == "citizens" && p.Role != "Мафия" && p.Alive) ||
			(winner == "mafia" && p.Role == "Мафия" && p.Alive) {
			mu.Lock()
			if data, ok := playersData[p.UserID]; ok {
				data.Coins += 10
			}
			mu.Unlock()
		}
	}

	bot.Send(tgbotapi.NewMessage(game.ChatID, text))
	delete(games, game.ID)
}

func showProfile(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	p, ok := playersData[userID]
	mu.RUnlock()
	if !ok {
		return
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Profile\n\nName: %s\nCoins: %d\nDefense: %d", p.FirstName, p.Coins, p.Defense)))
}

func showShop(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	p, ok := playersData[userID]
	mu.RUnlock()
	if !ok {
		return
	}
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Shop\n\nCoins: %d\n\nArmor - 15 coins\nRevive - 30 coins", p.Coins)))
}

func showHelp(bot *tgbotapi.BotAPI, chatID int64) {
	bot.Send(tgbotapi.NewMessage(chatID, "MAFIA HELP\n\n/game - Create game\n/join - Join\n/stop - Stop\n/skip - Skip timer\n/profile - Profile\n/shop - Shop\n\nWinner gets +10 coins!"))
}

func getAlive(game *Game) []*Player {
	var alive []*Player
	for _, p := range game.Players {
		if p.Alive {
			alive = append(alive, p)
		}
	}
	return alive
}

func getRoleDesc(role string) string {
	descs := map[string]string{
		"Мафия":        "Kill citizens at night",
		"Мирный житель": "Vote during day",
		"Доктор":       "Save one player each night",
		"Детектив":     "Check one player's role each night",
	}
	if d, ok := descs[role]; ok {
		return d
	}
	return "Citizen"
}
