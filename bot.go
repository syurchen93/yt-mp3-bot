package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

//go:embed config.json
var configFile embed.FS
var maxFileSize int64 = 49 * 1024 * 1024 // 50 MB

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

	if !isValidYouTubeURL(url) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Please send a valid YouTube video URL.")
		_, err := bot.Send(msg)
		if err != nil {
			log.Println("Error sending message:", err)
		}
		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, "Starting to process your request...")
	_, err := bot.Send(msg)
	if err != nil {
		log.Println("Error sending message:", err)
	}

	mp3FilePath, m4aFilePath, err := downloadMp3(url, message.Chat.ID)
	if err != nil {
		errorMsg := tgbotapi.NewMessage(message.Chat.ID, "Error downloading mp3: "+err.Error())
		_, err = bot.Send(errorMsg)
		if err != nil {
			log.Println("Error sending message:", err)
		}
		log.Println("Error downloading mp3:", err)
		return
	}

	err = checkAndSendFile(mp3FilePath, message.Chat.ID, bot)
	if err != nil {
		errorMsg := tgbotapi.NewMessage(message.Chat.ID, "Error sending mp3: "+err.Error())
		_, err = bot.Send(errorMsg)
		if err != nil {
			log.Println("Error sending message:", err)
		}
		log.Println("Error sending mp3:", err)
	}

	os.Remove(m4aFilePath)
}

func sendFile(bot *tgbotapi.BotAPI, filePath string, chatID int64) error {
	audioFile := tgbotapi.NewAudio(chatID, tgbotapi.FilePath(filePath))
	_, err := bot.Send(audioFile)
	os.Remove(filePath)

	return err
}

func checkAndSendFile(filePath string, chatID int64, bot *tgbotapi.BotAPI) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("could not check file size: %v", err)
	}

	if fileInfo.Size() > maxFileSize {
		log.Println("File exceeds 50 MB, splitting into parts")
		partFiles, err := splitFile(filePath, maxFileSize)
		if err != nil {
			return fmt.Errorf("error splitting file: %v", err)
		}

		for _, part := range partFiles {
			err := sendFile(bot, part, chatID)
			if err != nil {
				return fmt.Errorf("error sending file part: %v", err)
			}
		}
	} else {
		err := sendFile(bot, filePath, chatID)
		if err != nil {
			return fmt.Errorf("error sending file: %v", err)
		}
	}

	return nil
}

func splitFile(filePath string, chunkSize int64) ([]string, error) {
	var partFiles []string

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	totalSize := fileInfo.Size()

	for i := int64(0); i < totalSize; i += chunkSize {
		partFilename := fmt.Sprintf("%s.part%d", filePath, i/chunkSize)
		partFile, err := os.Create(partFilename)
		if err != nil {
			return nil, fmt.Errorf("could not create part file: %v", err)
		}

		_, err = io.CopyN(partFile, file, chunkSize)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("error copying part: %v", err)
		}
		partFile.Close()

		partFiles = append(partFiles, partFilename)
	}

	return partFiles, nil
}

func isValidYouTubeURL(url string) bool {
	return strings.Contains(url, "youtube.com/watch") || strings.Contains(url, "youtu.be/")
}

func downloadMp3(url string, chatID int64) (string, string, error) {
	timestamp := time.Now().UnixNano()
	filenameTemplate := fmt.Sprintf("download_%d_%d.%%(ext)s", chatID, timestamp)

	cmd := exec.Command("yt-dlp", "-x", "--audio-format", "mp3", "-o", filenameTemplate, url)

	output, err := cmd.CombinedOutput()

	log.Printf("yt-dlp output: %s", output)

	if err != nil {
		log.Println("Error executing yt-dlp:", err)
		return "", "", err
	}

	mp3Filename := fmt.Sprintf("download_%d_%d.mp3", chatID, timestamp)
	m4aFilename := fmt.Sprintf("download_%d_%d.m4a", chatID, timestamp)

	return mp3Filename, m4aFilename, nil
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
