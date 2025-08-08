package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	nb "github.com/Yakiyo/nekos_best.go"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/robfig/cron/v3"
)

const (
	CHAT_ID  = "-1001932178615"
	TIMEZONE = "Asia/Jakarta"
)

type Config struct {
	BotToken string
}

func loadConfig() (*Config, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("missing BOT_TOKEN environment variable")
	}
	return &Config{BotToken: token}, nil
}

func escapeHTML(str string) string {
	return html.EscapeString(str)
}

func sendDailyNeko(ctx context.Context, b *bot.Bot) error {
	log.Println("Fetching neko images...")
	
	results, err := nb.FetchMany("neko", 10)
	if err != nil {
		return fmt.Errorf("failed to fetch neko images: %w", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no images received from API")
	}

	log.Printf("Received %d neko images, preparing to send...", len(results))

	var mediaGroup []models.InputMedia
	for _, item := range results {
		caption := fmt.Sprintf(
			"Artist: <a href=\"%s\">%s</a>\nSource: <a href=\"%s\">%s</a>",
			escapeHTML(item.Artist_href),
			escapeHTML(item.Artist_name),
			escapeHTML(item.Source_url),
			escapeHTML(item.Source_url),
		)

		media := &models.InputMediaPhoto{
			Media:     item.Url,
			Caption:   caption,
			ParseMode: models.ParseModeHTML,
		}
		mediaGroup = append(mediaGroup, media)
	}

	_, err = b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
		ChatID: CHAT_ID,
		Media:  mediaGroup,
	})

	if err != nil {
		return fmt.Errorf("failed to send media group: %w", err)
	}

	log.Printf("[%s] Successfully sent %d neko images", time.Now().Format("2006-01-02 15:04:05"), len(results))
	return nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal("Config error:", err)
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
		}),
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Bot started successfully")

	log.Println("Sending neko images immediately for testing...")
	if err := sendDailyNeko(ctx, b); err != nil {
		log.Printf("Failed to send neko images: %v", err)
	}

	location, err := time.LoadLocation(TIMEZONE)
	if err != nil {
		log.Printf("Warning: Failed to load timezone %s, using UTC: %v", TIMEZONE, err)
		location = time.UTC
	}

	c := cron.New(cron.WithLocation(location))
	
	_, err = c.AddFunc("0 6 * * *", func() {
		log.Println("Executing scheduled neko image sending...")
		if err := sendDailyNeko(ctx, b); err != nil {
			log.Printf("Scheduled task failed: %v", err)
		}
	})
	
	if err != nil {
		log.Fatal("Failed to schedule cron job:", err)
	}

	c.Start()
	log.Printf("Scheduled daily task at 06:00 %s", TIMEZONE)

	go b.Start(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	<-sigChan
	log.Println("Received interrupt signal, shutting down...")
	
	c.Stop()
	cancel()
	log.Println("Bot stopped")
}