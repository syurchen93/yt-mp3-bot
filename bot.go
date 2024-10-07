package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

//go:embed config.json
var configFile embed.FS

type Config struct {
	BotToken  string `json:"bot-token"`
	DebugMode bool   `json:"debug-mode"`
}

func main() {
	conf, err := loadConfig()
	if err != nil {
		panic(fmt.Errorf("error loading configuration: %v", err))
	}
	botToken := conf.BotToken
	fmt.Println("Bot token:", botToken)

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = conf.DebugMode

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			go handleMessage(bot, update.Message)
		}
	}
}

func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	url := strings.TrimSpace(message.Text)

	if isValidYouTubeURL(url) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Starting to process your request...")
		_, err := bot.Send(msg)
		if err != nil {
			log.Println("Error sending message:", err)
		}

		mp3FilePath, err := downloadMP3(url, message.Chat.ID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(message.Chat.ID, "Error downloading MP3: "+err.Error())
			_, err = bot.Send(errorMsg)
			if err != nil {
				log.Println("Error sending message:", err)
			}
			log.Println("Error downloading MP3:", err)
			return
		}

		audioFile := tgbotapi.NewAudio(message.Chat.ID, tgbotapi.FilePath(mp3FilePath))
		_, err = bot.Send(audioFile)
		if err != nil {
			log.Println("Error sending audio file:", err)
		}

		os.Remove(mp3FilePath)
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Please send a valid YouTube video URL.")
		_, err := bot.Send(msg)
		if err != nil {
			log.Println("Error sending message:", err)
		}
	}
}

func isValidYouTubeURL(url string) bool {
	return strings.Contains(url, "youtube.com/watch") || strings.Contains(url, "youtu.be/")
}

func downloadMP3(url string, chatID int64) (string, error) {
	timestamp := time.Now().UnixNano()
	filenameTemplate := fmt.Sprintf("download_%d_%d.%%(ext)s", chatID, timestamp)

	cmd := exec.Command("yt-dlp", "-x", "--audio-format", "mp3", "-o", filenameTemplate, url)

	output, err := cmd.CombinedOutput()

	log.Printf("yt-dlp output: %s", output)

	if err != nil {
		log.Println("Error executing yt-dlp:", err)
		return "", err
	}

	mp3Filename := fmt.Sprintf("download_%d_%d.mp3", chatID, timestamp)

	return mp3Filename, nil
}

func loadConfig() (*Config, error) {
	data, err := configFile.ReadFile("config.json")
	if err != nil {
		return nil, fmt.Errorf("could not read config: %v", err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("could not parse config: %v", err)
	}

	return &config, nil
}
