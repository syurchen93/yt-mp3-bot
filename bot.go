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
var (
	maxFileSize int64 = 49 * 1024 * 1024 // 50 MB
	bitrateKBps       = 128
)

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
		partFiles, err := splitFile(filePath, maxFileSize, bitrateKBps)
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

func splitFile(filePath string, chunkSize int64, bitrateKbps int) ([]string, error) {
	var partFiles []string

	segmentTime := calculateSegmentTime(chunkSize, bitrateKbps)
	log.Printf("Splitting file with segment time of %d seconds", segmentTime)

	outputPattern := fmt.Sprintf("%s.part%%03d.mp3", filePath)

	cmd := exec.Command("ffmpeg", "-i", filePath, "-f", "segment", "-segment_time", fmt.Sprintf("%d", segmentTime), "-c", "copy", outputPattern)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error splitting file with ffmpeg: %s\n%s", err, string(output))
		return nil, err
	}

	i := 0
	for {
		partFilename := fmt.Sprintf("%s.part%03d.mp3", filePath, i)
		if err := exec.Command("test", "-f", partFilename).Run(); err != nil {
			break // No more parts exist
		}
		partFiles = append(partFiles, partFilename)
		i++
	}

	log.Printf("File split successfully into %d parts", len(partFiles))
	return partFiles, nil
}

func calculateSegmentTime(chunkSize int64, bitrateKbps int) int {
	chunkSizeKB := chunkSize / 1024

	bitrateKBps := int64(bitrateKbps / 8)

	segmentTime := chunkSizeKB / bitrateKBps
	return int(segmentTime)
}

func isValidYouTubeURL(url string) bool {
	return strings.Contains(url, "youtube.com/watch") || strings.Contains(url, "youtu.be/")
}

func downloadMp3(url string, chatID int64) (string, string, error) {
	timestamp := time.Now().UnixNano()
	filenameTemplate := fmt.Sprintf("download_%d_%d.%%(ext)s", chatID, timestamp)

	cmd := exec.Command(
		"yt-dlp",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", fmt.Sprintf("%dK", bitrateKBps),
		"-o", filenameTemplate,
		url,
	)

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
