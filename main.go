package main

import (
	"fmt"
	"log"
	"math/rand"
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
	Protected bool
	Coins     int
	Defense   int
	Votes     int // Для двойного голоса
	Silenced  bool // Молчание от Саботажника
}

type Game struct {
	ID            string
	ChatID        int64
	Players       map[int64]*Player
	Phase         string
	Round         int
	MinPlayers    int
	MaxPlayers    int
	StartTime     time.Time
	Votes         map[int64]int64
	MafiaTarget   int64
	DoctorSave    int64
	CreatedBy     int64
	IsRunning     bool
	JoinMessageID int
}

type ShopItem struct {
	Name        string
	Description string
	Price       int
	Type        string
}

var (
	games       = make(map[string]*Game)
	playersData = make(map[int64]*Player)
	mu          sync.RWMutex
	gameCounter int
	shopItems   []ShopItem
)

func init() {
	rand.Seed(time.Now().UnixNano())

	shopItems = []ShopItem{
		{Name: "🛡️ Бронежилет", Description: "Защита от одного убийства", Price: 15, Type: "defense"},
		{Name: "💊 Аптечка", Description: "Воскрешение после смерти", Price: 30, Type: "revive"},
		{Name: "🔮 Хрустальный шар", Description: "Узнать роль игрока", Price: 20, Type: "reveal"},
		{Name: "⚡ Двойной голос", Description: "Ваш голос считается за 2", Price: 25, Type: "double_vote"},
	}
}

func main() {
	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false
	
	// Убираем старые обновления
	bot.Request(tgbotapi.DeleteWebhookConfig{})

	fmt.Println("╔════════════════════════════════╗")
	fmt.Println("║     🎭 МАФИЯ БОТ ЗАПУЩЕН      ║")
	fmt.Println("╠════════════════════════════════╣")
	fmt.Printf("║ Бот: @%s\n", bot.Self.UserName)
	fmt.Println("║ Режим: Long Polling           ║")
	fmt.Println("╚════════════════════════════════╝")
	fmt.Println()
	fmt.Println("📋 Доступные роли:")
	fmt.Println("  🔪 Мафия        - убивает ночью")
	fmt.Println("  👤 Мирный житель - голосует днём")
	fmt.Println("  💊 Доктор       - лечит ночью")
	fmt.Println("  🔍 Детектив     - проверяет роль")
	fmt.Println("  🃏 Джокер       - играет сам за себя")
	fmt.Println("  💣 Террорист    - взрывает при смерти")
	fmt.Println("  🎭 Актёр        - маскируется под другую роль")
	fmt.Println("  🌙 Вампир       - обращает игроков")
	fmt.Println("  🧟 Зомби        - воскресает один раз")
	fmt.Println("  🛡️ Телохранитель - защищает игрока")
	fmt.Println()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMessage(bot, update.Message)
		}
		if update.CallbackQuery != nil {
			handleCallback(bot, update.CallbackQuery)
		}
	}
}

func handleMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()

	// Инициализация игрока
	mu.Lock()
	if _, exists := playersData[userID]; !exists {
		playersData[userID] = &Player{
			UserID:    userID,
			Username:  msg.From.UserName,
			FirstName: msg.From.FirstName,
			Coins:     50,
			Defense:   0,
		}
	}
	mu.Unlock()

	switch {
	case text == "/start" || text == "/start@Criprocfgbot":
		if isGroup {
			sendGroupWelcome(bot, chatID, msg.From.FirstName)
		} else {
			sendPrivateWelcome(bot, chatID, msg.From.FirstName)
		}

	case text == "/game" || text == "/game@Criprocfgbot":
		if isGroup {
			startGame(bot, chatID, userID, msg.From)
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ /game работает только в группах!"))
		}

	case text == "/join" || text == "/join@Criprocfgbot":
		if isGroup {
			joinGameGroup(bot, chatID, userID, msg.From)
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ /join работает только в группах!"))
		}

	case text == "/profile" || text == "/profile@Criprocfgbot":
		showProfile(bot, chatID, userID)

	case text == "/shop" || text == "/shop@Criprocfgbot":
		showShop(bot, chatID, userID)

	case text == "/roles" || text == "/roles@Criprocfgbot":
		showRoles(bot, chatID)

	case text == "/help" || text == "/help@Criprocfgbot":
		showHelp(bot, chatID)
	}
}

func handleCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	data := cb.Data
	parts := strings.Split(data, "|")
	
	if len(parts) < 2 {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Ошибка данных"))
		return
	}

	action := parts[0]
	gameID := parts[1]
	userID := cb.From.ID

	mu.RLock()
	game, exists := games[gameID]
	mu.RUnlock()

	if !exists && action != "shop_buy" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Игра не найдена"))
		return
	}

	switch action {
	case "join":
		handleJoinButton(bot, cb, game, userID)
	case "vote":
		if len(parts) >= 4 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			handleVote(bot, cb, game, userID, targetID)
		}
	case "mafia_kill":
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			handleMafiaKill(bot, cb, game, userID, targetID)
		}
	case "doctor_save":
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			handleDoctorSave(bot, cb, game, userID, targetID)
		}
	case "detective_check":
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			handleDetectiveCheck(bot, cb, game, userID, targetID)
		}
	case "joker_guess":
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			handleJokerGuess(bot, cb, game, userID, targetID)
		}
	case "shop_buy":
		if len(parts) >= 3 {
			itemIndex, _ := strconv.Atoi(parts[2])
			buyShopItem(bot, cb, userID, itemIndex)
		}
	}
}

// ======================
// JOIN BUTTON HANDLER
// ======================

func handleJoinButton(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID int64) {
	mu.Lock()
	defer mu.Unlock()

	// Проверяем что игра в фазе ожидания
	if game.Phase != "waiting" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Игра уже началась!"))
		return
	}

	// Проверяем что игрок не в игре
	if _, exists := game.Players[userID]; exists {
		bot.Request(tgbotapi.NewCallback(cb.ID, "⚠️ Вы уже в игре!"))
		return
	}

	// Добавляем игрока
	user := cb.From
	game.Players[userID] = &Player{
		UserID:    userID,
		Username:  user.UserName,
		FirstName: user.FirstName,
		Alive:     true,
		Defense:   0,
		Coins:     0,
	}

	// Синхронизируем монеты и защиту
	mu.Lock()
	if data, ok := playersData[userID]; ok {
		game.Players[userID].Coins = data.Coins
		game.Players[userID].Defense = data.Defense
	}
	mu.Unlock()

	playerCount := len(game.Players)

	// Отправляем ЛС игроку
	privateMsg := tgbotapi.NewMessage(userID, 
		fmt.Sprintf("✅ Вы присоединились к игре Мафия!\n👥 Игроков: %d\n⏳ Ожидайте начала...", playerCount))
	bot.Send(privateMsg)

	// Обновляем сообщение в группе
	updateText := fmt.Sprintf(
		"🎭 **МАФИЯ**\n\n⏳ Идёт набор игроков...\n👥 Присоединилось: %d\n⏰ До начала: ~%d сек\n\n"+
		"Нажмите кнопку чтобы присоединиться!",
		playerCount, 
		60-time.Now().Sub(game.StartTime).Seconds(),
	)

	editMsg := tgbotapi.NewEditMessageText(game.ChatID, game.JoinMessageID, updateText)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("🎮 Присоединиться", fmt.Sprintf("join|%s", game.ID))},
		},
	}
	bot.Send(editMsg)

	// Уведомление в группе
	notifyMsg := tgbotapi.NewMessage(game.ChatID, 
		fmt.Sprintf("✅ [%s](tg://user?id=%d) вошёл в игру! (%d игроков)", 
			user.FirstName, userID, playerCount))
	notifyMsg.ParseMode = "Markdown"
	notifyMsg.DisableWebPagePreview = true
	bot.Send(notifyMsg)

	bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Вы в игре!"))

	fmt.Printf("👤 %s (ID:%d) присоединился к игре %s (%d/%d)\n", 
		user.FirstName, userID, game.ID, playerCount, game.MaxPlayers)
}

// ======================
// GAME CREATION
// ======================

func startGame(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
	mu.Lock()
	defer mu.Unlock()

	// Проверяем нет ли активной игры
	for _, g := range games {
		if g.ChatID == chatID && g.Phase != "ended" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ В этом чате уже идёт игра!"))
			return
		}
	}

	gameID := fmt.Sprintf("g%d_%d", chatID, gameCounter)
	gameCounter++

	game := &Game{
		ID:         gameID,
		ChatID:     chatID,
		Players:    make(map[int64]*Player),
		Phase:      "waiting",
		Round:      0,
		MinPlayers: 3,
		MaxPlayers: 999,
		Votes:      make(map[int64]int64),
		CreatedBy:  userID,
		IsRunning:  false,
		StartTime:  time.Now(),
	}

	games[gameID] = game

	// Отправляем сообщение с кнопкой присоединения
	msgText := fmt.Sprintf(
		"🎭 **МАФИЯ**\n\n🎮 Игра создана!\n👤 Создал: [%s](tg://user?id=%d)\n\n"+
		"⏳ Набор игроков: 60 секунд\n👥 Минимум: 3 игрока\n\nНажмите кнопку чтобы присоединиться!",
		user.FirstName, userID,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎮 Присоединиться к игре", fmt.Sprintf("join|%s", gameID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sent, err := bot.Send(msg)
	
	if err != nil {
		fmt.Printf("❌ Ошибка отправки: %v\n", err)
		delete(games, gameID)
		return
	}

	game.JoinMessageID = sent.MessageID

	fmt.Printf("\n🎮 Игра %s создана в чате %d игроком %s\n", gameID, chatID, user.FirstName)

	// Таймер на 60 секунд
	go gameTimer(bot, game)
}

func gameTimer(bot *tgbotapi.BotAPI, game *Game) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeout := time.After(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			mu.RLock()
			playerCount := len(game.Players)
			remaining := 60 - time.Now().Sub(game.StartTime).Seconds()
			mu.RUnlock()

			if remaining > 0 && game.Phase == "waiting" {
				updateText := fmt.Sprintf(
					"🎭 **МАФИЯ**\n\n⏳ Набор игроков...\n👥 Присоединилось: %d\n⏰ Осталось: %d сек\n\n"+
					"Нажмите кнопку чтобы присоединиться!",
					playerCount, int(remaining),
				)

				mu.RLock()
				editMsg := tgbotapi.NewEditMessageText(game.ChatID, game.JoinMessageID, updateText)
				editMsg.ParseMode = "Markdown"
				editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
					InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
						{tgbotapi.NewInlineKeyboardButtonData("🎮 Присоединиться", fmt.Sprintf("join|%s", game.ID))},
					},
				}
				mu.RUnlock()
				bot.Send(editMsg)
			}

		case <-timeout:
			mu.Lock()
			defer mu.Unlock()

			if game.Phase != "waiting" {
				return
			}

			if len(game.Players) < game.MinPlayers {
				msg := tgbotapi.NewMessage(game.ChatID, 
					fmt.Sprintf("❌ Игра отменена! Недостаточно игроков (%d из %d)", 
						len(game.Players), game.MinPlayers))
				bot.Send(msg)
				delete(games, game.ID)
				return
			}

			// Запускаем игру
			startMafiaGame(bot, game)
			return
		}
	}
}

// ======================
// GAME START
// ======================

func startMafiaGame(bot *tgbotapi.BotAPI, game *Game) {
	game.IsRunning = true
	game.Phase = "night"
	game.Round = 1

	// Назначаем роли
	assignRoles(game)

	// Выводим роли в консоль
	fmt.Println("\n╔════════════════════════════════╗")
	fmt.Printf("║  🎭 ИГРА %s  ║\n", game.ID)
	fmt.Println("╠════════════════════════════════╣")
	fmt.Printf("║ Чат: %d\n", game.ChatID)
	fmt.Printf("║ Игроков: %d\n", len(game.Players))
	fmt.Println("╠════════════════════════════════╣")
	fmt.Println("║ РОЛИ:                         ║")
	for _, player := range game.Players {
		name := player.FirstName
		if player.Username != "" {
			name = "@" + player.Username
		}
		fmt.Printf("║ %-20s → %s\n", name, player.Role)
	}
	fmt.Println("╚════════════════════════════════╝")
	fmt.Println()

	// Отправляем роли в ЛС каждому игроку
	for _, player := range game.Players {
		roleMsg := fmt.Sprintf(
			"🎭 **МАФИЯ - ВАША РОЛЬ**\n\n"+
			"Роль: **%s**\n\n"+
			"%s\n\n"+
			"🎮 Игра началась! Ждите ночной фазы.",
			player.Role,
			getRoleDescription(player.Role),
		)

		msg := tgbotapi.NewMessage(player.UserID, roleMsg)
		msg.ParseMode = "Markdown"
		bot.Send(msg)

		fmt.Printf("📨 Роль для %s (ID:%d): %s\n", player.FirstName, player.UserID, player.Role)
	}

	// Объявляем начало в группе
	playersList := getPlayersList(game)
	announceMsg := tgbotapi.NewMessage(game.ChatID,
		fmt.Sprintf("🎭 **МАФИЯ НАЧИНАЕТСЯ!**\n\n🌙 **НОЧЬ %d**\n\nГород засыпает...\n\nИгроки:\n%s\n\nМафия, Доктор, Детектив - проверьте ЛС!",
			game.Round, playersList))
	announceMsg.ParseMode = "Markdown"
	announceMsg.DisableWebPagePreview = true
	bot.Send(announceMsg)

	// Удаляем сообщение о наборе
	bot.Send(tgbotapi.NewDeleteMessage(game.ChatID, game.JoinMessageID))

	// Ночная фаза
	nightPhase(bot, game)
}

func assignRoles(game *Game) {
	playerCount := len(game.Players)
	playerIDs := make([]int64, 0, playerCount)
	for id := range game.Players {
		playerIDs = append(playerIDs, id)
	}

	rand.Shuffle(len(playerIDs), func(i, j int) {
		playerIDs[i], playerIDs[j] = playerIDs[j], playerIDs[i]
	})

	// Расчет ролей
	mafiaCount := playerCount / 3
	if mafiaCount < 1 {
		mafiaCount = 1
	}
	if mafiaCount > 3 {
		mafiaCount = 3
	}

	// Список всех ролей
	allRoles := []string{}
	
	// Мафия
	for i := 0; i < mafiaCount; i++ {
		allRoles = append(allRoles, "Мафия")
	}
	
	// Доктор (всегда 1)
	allRoles = append(allRoles, "Доктор")
	
	// Детектив (если больше 4 игроков)
	if playerCount > 4 {
		allRoles = append(allRoles, "Детектив")
	}
	
	// Джокер (если больше 5)
	if playerCount > 5 {
		allRoles = append(allRoles, "Джокер")
	}
	
	// Телохранитель (если больше 6)
	if playerCount > 6 {
		allRoles = append(allRoles, "Телохранитель")
	}
	
	// Вампир (если больше 7)
	if playerCount > 7 {
		allRoles = append(allRoles, "Вампир")
	}
	
	// Террорист (если больше 8)
	if playerCount > 8 {
		allRoles = append(allRoles, "Террорист")
	}
	
	// Актёр (если больше 9)
	if playerCount > 9 {
		allRoles = append(allRoles, "Актёр")
	}
	
	// Зомби (если больше 10)
	if playerCount > 10 {
		allRoles = append(allRoles, "Зомби")
	}
	
	// Остальные - мирные жители
	for len(allRoles) < playerCount {
		allRoles = append(allRoles, "Мирный житель")
	}

	// Перемешиваем еще раз
	rand.Shuffle(len(allRoles), func(i, j int) {
		allRoles[i], allRoles[j] = allRoles[j], allRoles[i]
	})

	// Назначаем роли
	for i, id := range playerIDs {
		if i < len(allRoles) {
			game.Players[id].Role = allRoles[i]
		}
	}
}

// ======================
// NIGHT PHASE
// ======================

func nightPhase(bot *tgbotapi.BotAPI, game *Game) {
	alivePlayers := getAlivePlayers(game)

	// Отправляем кнопки действий в ЛС
	for _, player := range alivePlayers {
		var buttons [][]tgbotapi.InlineKeyboardButton

		switch player.Role {
		case "Мафия":
			for _, target := range alivePlayers {
				if target.UserID != player.UserID && target.Role != "Мафия" {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("🔪 Убить %s", target.FirstName),
							fmt.Sprintf("mafia_kill|%s|%d", game.ID, target.UserID),
						),
					))
				}
			}

		case "Доктор":
			for _, target := range alivePlayers {
				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("💊 Лечить %s", target.FirstName),
						fmt.Sprintf("doctor_save|%s|%d", game.ID, target.UserID),
					),
				))
			}

		case "Детектив":
			for _, target := range alivePlayers {
				if target.UserID != player.UserID {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("🔍 Проверить %s", target.FirstName),
							fmt.Sprintf("detective_check|%s|%d", game.ID, target.UserID),
						),
					))
				}
			}

		case "Джокер":
			for _, target := range alivePlayers {
				if target.UserID != player.UserID {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("🃏 Угадать роль %s", target.FirstName),
							fmt.Sprintf("joker_guess|%s|%d", game.ID, target.UserID),
						),
					))
				}
			}

		case "Телохранитель":
			for _, target := range alivePlayers {
				if target.UserID != player.UserID {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("🛡️ Защитить %s", target.FirstName),
							fmt.Sprintf("doctor_save|%s|%d", game.ID, target.UserID),
						),
					))
				}
			}
		}

		if len(buttons) > 0 {
			keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
			actionMsg := tgbotapi.NewMessage(player.UserID, 
				fmt.Sprintf("🌙 Ночь %d\nВаша роль: %s\n\nВыберите действие:", game.Round, player.Role))
			actionMsg.ReplyMarkup = keyboard
			bot.Send(actionMsg)
		}
	}

	// Таймер ночной фазы
	go func() {
		time.Sleep(35 * time.Second)
		
		mu.Lock()
		defer mu.Unlock()
		
		if game.Phase == "night" {
			processNightActions(bot, game)
		}
	}()
}

func processNightActions(bot *tgbotapi.BotAPI, game *Game) {
	fmt.Printf("\n🌙 Обработка ночи %d для игры %s\n", game.Round, game.ID)
	
	victim := game.MafiaTarget
	saved := (game.DoctorSave == victim)
	exploded := false

	// Обработка убийства
	if victim != 0 {
		if player, ok := game.Players[victim]; ok {
			if saved {
				fmt.Printf("  💊 %s был спасён доктором!\n", player.FirstName)
				bot.Send(tgbotapi.NewMessage(game.ChatID, 
					fmt.Sprintf("💊 Этой ночью кто-то был спасён доктором!")))
			} else if player.Defense > 0 {
				player.Defense--
				fmt.Printf("  🛡️ %s защитился бронежилетом\n", player.FirstName)
				bot.Send(tgbotapi.NewMessage(game.ChatID, 
					fmt.Sprintf("🛡️ %s был атакован, но защита спасла его!", getPlayerName(player))))
			} else if player.Role == "Террорист" {
				player.Alive = false
				exploded = true
				fmt.Printf("  💣 %s взорвался при смерти!\n", player.FirstName)
				bot.Send(tgbotapi.NewMessage(game.ChatID, 
					fmt.Sprintf("💣 %s был убит и ВЗОРВАЛСЯ!", getPlayerName(player))))
			} else {
				player.Alive = false
				fmt.Printf("  💀 %s был убит\n", player.FirstName)
				bot.Send(tgbotapi.NewMessage(game.ChatID, 
					fmt.Sprintf("💀 %s был убит этой ночью!", getPlayerName(player))))
			}
		}
	}

	// Если террорист взорвался - убиваем случайного мафию
	if exploded {
		alivePlayers := getAlivePlayers(game)
		var mafiaPlayers []*Player
		for _, p := range alivePlayers {
			if p.Role == "Мафия" {
				mafiaPlayers = append(mafiaPlayers, p)
			}
		}
		if len(mafiaPlayers) > 0 {
			randomMafia := mafiaPlayers[rand.Intn(len(mafiaPlayers))]
			randomMafia.Alive = false
			fmt.Printf("  💀 Террорист забрал с собой %s\n", randomMafia.FirstName)
			bot.Send(tgbotapi.NewMessage(game.ChatID, 
				fmt.Sprintf("💣 Взрыв убил мафию: %s!", getPlayerName(randomMafia))))
		}
	}

	// Проверка зомби
	for _, player := range game.Players {
		if !player.Alive && player.Role == "Зомби" && !player.Protected {
			player.Alive = true
			player.Protected = true
			fmt.Printf("  🧟 %s воскрес как зомби!\n", player.FirstName)
			bot.Send(tgbotapi.NewMessage(game.ChatID, 
				fmt.Sprintf("🧟 %s ВОСКРЕС!")))
			break
		}
	}

	// Проверка победы
	if checkWinCondition(bot, game) {
		return
	}

	// Дневная фаза
	game.Phase = "day"
	game.Votes = make(map[int64]int64)
	game.MafiaTarget = 0
	game.DoctorSave = 0

	dayPhase(bot, game)
}

// ======================
// DAY PHASE
// ======================

func dayPhase(bot *tgbotapi.BotAPI, game *Game) {
	alivePlayers := getAlivePlayers(game)
	playersList := getAlivePlayersList(game)

	fmt.Printf("\n☀️ День %d для игры %s\n", game.Round, game.ID)

	msgText := fmt.Sprintf(
		"☀️ **ДЕНЬ %d**\n\nЖивые игроки:\n%s\n\n🗳️ Голосование! Выберите кого казнить:",
		game.Round, playersList,
	)

	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, player := range alivePlayers {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🗳️ %s", getPlayerName(player)),
				fmt.Sprintf("vote|%s|%d", game.ID, player.UserID),
			),
		))
	}

	// Кнопка пропуска голосования
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⏭️ Пропустить голосование", fmt.Sprintf("vote|%s|0", game.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(game.ChatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)

	// Таймер дневной фазы
	go func() {
		time.Sleep(50 * time.Second)
		
		mu.Lock()
		defer mu.Unlock()
		
		if game.Phase == "day" {
			processVoting(bot, game)
		}
	}()
}

func handleVote(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID, targetID int64) {
	mu.Lock()
	defer mu.Unlock()

	if player, ok := game.Players[userID]; !ok || !player.Alive {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Вы не можете голосовать!"))
		return
	}

	// Если пропуск голосования
	if targetID == 0 {
		game.Votes[userID] = 0
		bot.Request(tgbotapi.NewCallback(cb.ID, "⏭️ Вы пропустили голосование"))
		return
	}

	// Проверяем цель
	if target, ok := game.Players[targetID]; !ok || !target.Alive {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Этот игрок мёртв!"))
		return
	}

	// Учитываем двойной голос
	votePower := 1
	if game.Players[userID].Votes > 0 {
		votePower = 2
		game.Players[userID].Votes--
	}

	game.Votes[userID] = targetID
	
	voteCount := make(map[int64]int)
	for _, t := range game.Votes {
		if t != 0 {
			voteCount[t]++
		}
	}

	resultText := fmt.Sprintf("✅ %s проголосовал за %s", 
		game.Players[userID].FirstName, game.Players[targetID].FirstName)
	if votePower == 2 {
		resultText += " (x2 голос)"
	}
	
	bot.Request(tgbotapi.NewCallback(cb.ID, resultText))
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
	for targetID, count := range voteCount {
		if count > maxVotes {
			maxVotes = count
			eliminated = targetID
		}
	}

	totalAlive := len(getAlivePlayers(game))

	if eliminated != 0 && maxVotes > totalAlive/3 {
		player := game.Players[eliminated]
		
		if player.Role == "Джокер" {
			// Джокер выиграл - его казнили
			bot.Send(tgbotapi.NewMessage(game.ChatID, 
				fmt.Sprintf("🃏 %s был казнен! Но это ДЖОКЕР! Он победил!", getPlayerName(player))))
			endGame(bot, game, "joker")
			return
		}
		
		player.Alive = false
		bot.Send(tgbotapi.NewMessage(game.ChatID, 
			fmt.Sprintf("🗳️ %s казнён! Его роль: **%s**", getPlayerName(player), player.Role)))
		
		fmt.Printf("  🗳️ %s казнён (%s)\n", player.FirstName, player.Role)
	} else {
		bot.Send(tgbotapi.NewMessage(game.ChatID, "🗳️ Никто не казнён. Недостаточно голосов."))
	}

	if checkWinCondition(bot, game) {
		return
	}

	// Следующий раунд
	game.Round++
	game.Votes = make(map[int64]int64)
	game.MafiaTarget = 0
	game.DoctorSave = 0
	game.Phase = "night"

	nightPhase(bot, game)
}

// ======================
// NIGHT ACTIONS HANDLERS
// ======================

func handleMafiaKill(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID, targetID int64) {
	mu.Lock()
	defer mu.Unlock()

	if _, ok := game.Players[userID]; !ok || game.Players[userID].Role != "Мафия" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Вы не мафия!"))
		return
	}

	game.MafiaTarget = targetID
	bot.Request(tgbotapi.NewCallback(cb.ID, "🔪 Цель выбрана"))

	fmt.Printf("  🔪 Мафия %s выбрала цель: %s\n", 
		game.Players[userID].FirstName, game.Players[targetID].FirstName)
}

func handleDoctorSave(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID, targetID int64) {
	mu.Lock()
	defer mu.Unlock()

	if player, ok := game.Players[userID]; !ok || (player.Role != "Доктор" && player.Role != "Телохранитель") {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Вы не можете лечить!"))
		return
	}

	game.DoctorSave = targetID
	bot.Request(tgbotapi.NewCallback(cb.ID, "💊 Защита выбрана"))

	fmt.Printf("  💊 %s выбрал защиту: %s\n", 
		game.Players[userID].FirstName, game.Players[targetID].FirstName)
}

func handleDetectiveCheck(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID, targetID int64) {
	mu.Lock()
	defer mu.Unlock()

	if player, ok := game.Players[userID]; !ok || player.Role != "Детектив" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Вы не детектив!"))
		return
	}

	target := game.Players[targetID]
	role := target.Role
	
	// Актёр маскируется
	if target.Role == "Актёр" {
		otherRoles := []string{"Мирный житель", "Доктор", "Детектив"}
		role = otherRoles[rand.Intn(len(otherRoles))]
	}

	msg := tgbotapi.NewMessage(userID, 
		fmt.Sprintf("🔍 Результат проверки %s:\n\nРоль: **%s**", target.FirstName, role))
	msg.ParseMode = "Markdown"
	bot.Send(msg)

	bot.Request(tgbotapi.NewCallback(cb.ID, "🔍 Проверка выполнена"))

	fmt.Printf("  🔍 Детектив %s проверил %s → %s\n", 
		game.Players[userID].FirstName, target.FirstName, role)
}

func handleJokerGuess(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, game *Game, userID, targetID int64) {
	mu.Lock()
	defer mu.Unlock()

	if player, ok := game.Players[userID]; !ok || player.Role != "Джокер" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Вы не джокер!"))
		return
	}

	target := game.Players[targetID]
	
	msg := tgbotapi.NewMessage(userID, 
		fmt.Sprintf("🃏 Роль игрока %s: **%s**", target.FirstName, target.Role))
	msg.ParseMode = "Markdown"
	bot.Send(msg)

	bot.Request(tgbotapi.NewCallback(cb.ID, "🃏 Роль угадана"))
}

// ======================
// WIN CONDITIONS
// ======================

func checkWinCondition(bot *tgbotapi.BotAPI, game *Game) bool {
	alive := getAlivePlayers(game)
	
	var mafiaCount, citizenCount int
	for _, p := range alive {
		if p.Role == "Мафия" || p.Role == "Вампир" {
			mafiaCount++
		} else if p.Role != "Джокер" {
			citizenCount++
		}
	}

	// Мафия победила
	if mafiaCount >= citizenCount && citizenCount > 0 {
		endGame(bot, game, "mafia")
		return true
	}

	// Мирные победили
	if mafiaCount == 0 && citizenCount > 0 {
		endGame(bot, game, "citizens")
		return true
	}

	return false
}

func endGame(bot *tgbotapi.BotAPI, game *Game, winner string) {
	game.Phase = "ended"

	fmt.Printf("\n🏆 Игра %s завершена! Победитель: %s\n", game.ID, winner)

	var resultText string
	switch winner {
	case "citizens":
		resultText = "🎉 **МИРНЫЕ ЖИТЕЛИ ПОБЕДИЛИ!**\n\nМафия уничтожена!"
	case "mafia":
		resultText = "💀 **МАФИЯ ПОБЕДИЛА!**\n\nМафия захватила город!"
	case "joker":
		resultText = "🃏 **ДЖОКЕР ПОБЕДИЛ!**\n\nЕго казнили и он выиграл!"
	}

	resultText += "\n\n**Результаты:**\n"
	
	for _, player := range game.Players {
		status := "💀"
		if player.Alive {
			status = "✅"
		}
		resultText += fmt.Sprintf("%s %s - **%s**\n", 
			status, getPlayerName(player), player.Role)

		// Начисляем монеты
		shouldGetCoins := false
		switch winner {
		case "citizens":
			shouldGetCoins = (player.Role != "Мафия" && player.Role != "Вампир")
		case "mafia":
			shouldGetCoins = (player.Role == "Мафия" || player.Role == "Вампир")
		case "joker":
			shouldGetCoins = (player.Role == "Джокер")
		}

		if shouldGetCoins && player.Alive {
			mu.Lock()
			if data, ok := playersData[player.UserID]; ok {
				data.Coins += 10
				player.Coins = data.Coins
				fmt.Printf("  💰 +10 монет: %s\n", player.FirstName)
			}
			mu.Unlock()
		}
	}

	msg := tgbotapi.NewMessage(game.ChatID, resultText)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	bot.Send(msg)

	delete(games, game.ID)
}

// ======================
// SHOP
// ======================

func showShop(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	player, ok := playersData[userID]
	mu.RUnlock()

	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Профиль не найден!"))
		return
	}

	shopText := fmt.Sprintf("🏪 **МАГАЗИН**\n💰 Монеты: %d\n\n", player.Coins)

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, item := range shopItems {
		shopText += fmt.Sprintf("%d. %s - %d💰\n   %s\n\n", 
			i+1, item.Name, item.Price, item.Description)
		
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%s (%d💰)", item.Name, item.Price),
				fmt.Sprintf("shop_buy|0|%d", i),
			),
		))
	}

	msg := tgbotapi.NewMessage(chatID, shopText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	bot.Send(msg)
}

func buyShopItem(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, userID int64, itemIndex int) {
	mu.Lock()
	defer mu.Unlock()

	if itemIndex < 0 || itemIndex >= len(shopItems) {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Товар не найден"))
		return
	}

	player, ok := playersData[userID]
	if !ok {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Профиль не найден"))
		return
	}

	item := shopItems[itemIndex]

	if player.Coins < item.Price {
		bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Недостаточно монет!"))
		return
	}

	player.Coins -= item.Price

	switch item.Type {
	case "defense":
		player.Defense++
		bot.Request(tgbotapi.NewCallback(cb.ID, fmt.Sprintf("✅ Куплен %s! Защита: %d", item.Name, player.Defense)))
	case "revive":
		player.Alive = true
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Аптечка куплена!"))
	case "reveal":
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Шар куплен! Используйте в игре"))
	case "double_vote":
		player.Votes++
		bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Двойной голос куплен!"))
	}

	bot.Send(tgbotapi.NewMessage(userID, 
		fmt.Sprintf("✅ Куплено: %s\n💰 Осталось монет: %d", item.Name, player.Coins)))
}

// ======================
// HELPERS
// ======================

func sendGroupWelcome(bot *tgbotapi.BotAPI, chatID int64, firstName string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"👋 %s, добро пожаловать в **Мафию**!\n\n"+
		"🎮 /game - Создать игру\n"+
		"👤 /join - Присоединиться\n"+
		"📊 /profile - Профиль\n"+
		"🏪 /shop - Магазин\n"+
		"📋 /roles - Роли\n"+
		"❓ /help - Помощь",
		firstName))
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func sendPrivateWelcome(bot *tgbotapi.BotAPI, chatID int64, firstName string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"🎭 Добро пожаловать в **Мафию**, %s!\n\n"+
		"Добавьте бота в группу и напишите /game чтобы начать!\n\n"+
		"💰 Стартовые монеты: 50\n"+
		"🏪 /shop - Магазин предметов",
		firstName))
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showProfile(bot *tgbotapi.BotAPI, chatID, userID int64) {
	mu.RLock()
	player, ok := playersData[userID]
	mu.RUnlock()

	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Профиль не найден"))
		return
	}

	profileText := fmt.Sprintf(
		"👤 **ПРОФИЛЬ**\n\n"+
		"Имя: %s\n"+
		"💰 Монеты: %d\n"+
		"🛡️ Защита: %d\n"+
		"⚡ Двойных голосов: %d\n\n"+
		"Зарабатывайте монеты побеждая в играх!",
		player.FirstName, player.Coins, player.Defense, player.Votes,
	)

	msg := tgbotapi.NewMessage(chatID, profileText)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showRoles(bot *tgbotapi.BotAPI, chatID int64) {
	rolesText := "🎭 **РОЛИ В МАФИИ:**\n\n" +
		"🔪 **Мафия** - убивает ночью\n" +
		"👤 **Мирный житель** - голосует днём\n" +
		"💊 **Доктор** - лечит ночью\n" +
		"🔍 **Детектив** - проверяет роль ночью\n" +
		"🃏 **Джокер** - побеждает если его казнят\n" +
		"💣 **Террорист** - взрывается при смерти\n" +
		"🎭 **Актёр** - скрывает свою роль\n" +
		"🌙 **Вампир** - сторона мафии\n" +
		"🧟 **Зомби** - воскресает один раз\n" +
		"🛡️ **Телохранитель** - защищает игрока ночью"

	msg := tgbotapi.NewMessage(chatID, rolesText)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showHelp(bot *tgbotapi.BotAPI, chatID int64) {
	helpText := "🎭 **МАФИЯ - ПОМОЩЬ**\n\n" +
		"**Как играть:**\n" +
		"1. /game - создать игру\n" +
		"2. /join - присоединиться\n" +
		"3. Ждать 60 сек\n" +
		"4. Ночью роли действуют в ЛС\n" +
		"5. Днём голосование в чате\n\n" +
		"**Магазин:**\n" +
		"🛡️ Бронежилет (15💰)\n" +
		"💊 Аптечка (30💰)\n" +
		"🔮 Шар (20💰)\n" +
		"⚡ Двойной голос (25💰)"

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func joinGameGroup(bot *tgbotapi.BotAPI, chatID, userID int64, user *tgbotapi.User) {
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
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет активной игры для присоединения!"))
		return
	}

	if _, exists := game.Players[userID]; exists {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ [%s](tg://user?id=%d), вы уже в игре!", 
			user.FirstName, userID)))
		return
	}

	game.Players[userID] = &Player{
		UserID:    userID,
		Username:  user.UserName,
		FirstName: user.FirstName,
		Alive:     true,
	}

	bot.Send(tgbotapi.NewMessage(userID, 
		fmt.Sprintf("✅ Вы присоединились к игре! (%d игроков)", len(game.Players))))
	
	bot.Send(tgbotapi.NewMessage(chatID, 
		fmt.Sprintf("✅ [%s](tg://user?id=%d) в игре! (%d/%d)", 
			user.FirstName, userID, len(game.Players), game.MaxPlayers)))

	fmt.Printf("👤 %s присоединился к игре %s\n", user.FirstName, game.ID)
}

func getAlivePlayers(game *Game) []*Player {
	var alive []*Player
	for _, p := range game.Players {
		if p.Alive {
			alive = append(alive, p)
		}
	}
	return alive
}

func getPlayersList(game *Game) string {
	var list string
	for _, p := range game.Players {
		status := "✅"
		if !p.Alive {
			status = "💀"
		}
		list += fmt.Sprintf("%s [%s](tg://user?id=%d)\n", status, p.FirstName, p.UserID)
	}
	return list
}

func getAlivePlayersList(game *Game) string {
	var list string
	for _, p := range game.Players {
		if p.Alive {
			list += fmt.Sprintf("• [%s](tg://user?id=%d)\n", p.FirstName, p.UserID)
		}
	}
	return list
}

func getPlayerName(player *Player) string {
	if player.FirstName != "" {
		return player.FirstName
	}
	if player.Username != "" {
		return "@" + player.Username
	}
	return fmt.Sprintf("ID:%d", player.UserID)
}

func getRoleDescription(role string) string {
	descriptions := map[string]string{
		"Мафия":        "Вы мафия! Убивайте мирных жителей по ночам.",
		"Мирный житель": "Вы мирный житель! Голосуйте днём против мафии.",
		"Доктор":       "Вы доктор! Каждую ночь спасайте одного игрока.",
		"Детектив":     "Вы детектив! Проверяйте роли игроков ночью.",
		"Джокер":       "Вы джокер! Ваша цель - быть казнённым днём!",
		"Террорист":    "Вы террорист! При смерти взорвётесь и убьёте мафию!",
		"Актёр":        "Вы актёр! Детектив видит вас как мирного жителя.",
		"Вампир":       "Вы вампир! Играете за мафию.",
		"Зомби":        "Вы зомби! Воскреснете после первой смерти.",
		"Телохранитель": "Вы телохранитель! Защищайте игрока от смерти.",
	}
	return descriptions[role]
}