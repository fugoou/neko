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
	CHAT_ID         = "-1001932178615"
	TIMEZONE        = "Asia/Jakarta"
	MAX_RETRIES     = 3
	RETRY_DELAY     = 5 * time.Second
	REQUEST_TIMEOUT = 30 * time.Second
	FETCH_COUNT     = 10
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

func fetchNekoImagesWithRetry(ctx context.Context) ([]nb.NBResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, REQUEST_TIMEOUT)
	defer cancel()

	var results []nb.NBResponse
	var lastErr error

	for attempt := 1; attempt <= MAX_RETRIES; attempt++ {
		log.Printf("Attempt %d/%d: Fetching neko images...", attempt, MAX_RETRIES)
		
		results, lastErr = nb.FetchMany("neko", FETCH_COUNT)
		if lastErr == nil && len(results) > 0 {
			log.Printf("Successfully fetched %d neko images on attempt %d", len(results), attempt)
			return results, nil
		}

		if lastErr != nil {
			log.Printf("Attempt %d failed: %v", attempt, lastErr)
		} else {
			log.Printf("Attempt %d failed: no images received", attempt)
		}

		if attempt < MAX_RETRIES {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(RETRY_DELAY):
				continue
			}
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch neko images after %d attempts: %w", MAX_RETRIES, lastErr)
	}
	return nil, fmt.Errorf("failed to fetch neko images after %d attempts: no images received", MAX_RETRIES)
}

func prepareMediaGroup(results []nb.NBResponse) []models.InputMedia {
	var mediaGroup []models.InputMedia

	for _, item := range results {
		var caption string
		
		if item.Artist_name != "" && item.Artist_href != "" {
			caption = fmt.Sprintf("Artist: <a href=\"%s\">%s</a>", 
				escapeHTML(item.Artist_href), 
				escapeHTML(item.Artist_name))
		} else if item.Artist_name != "" {
			caption = fmt.Sprintf("Artist: %s", escapeHTML(item.Artist_name))
		}

		if item.Source_url != "" {
			if caption != "" {
				caption += "\n"
			}
			caption += fmt.Sprintf("Source: %s", escapeHTML(item.Source_url))
		}

		media := &models.InputMediaPhoto{
			Media:     item.Url,
			Caption:   caption,
			ParseMode: models.ParseModeHTML,
		}

		mediaGroup = append(mediaGroup, media)
	}

	return mediaGroup
}

func sendMediaGroupWithRetry(ctx context.Context, b *bot.Bot, mediaGroup []models.InputMedia) error {
	ctx, cancel := context.WithTimeout(ctx, REQUEST_TIMEOUT)
	defer cancel()

	var lastErr error

	for attempt := 1; attempt <= MAX_RETRIES; attempt++ {
		log.Printf("Attempt %d/%d: Sending media group...", attempt, MAX_RETRIES)

		_, lastErr = b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
			ChatID: CHAT_ID,
			Media:  mediaGroup,
		})

		if lastErr == nil {
			log.Printf("Successfully sent media group on attempt %d", attempt)
			return nil
		}

		log.Printf("Send attempt %d failed: %v", attempt, lastErr)

		if attempt < MAX_RETRIES {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(RETRY_DELAY):
				continue
			}
		}
	}

	return fmt.Errorf("failed to send media group after %d attempts: %w", MAX_RETRIES, lastErr)
}

func sendDailyNeko(ctx context.Context, b *bot.Bot) error {
	startTime := time.Now()
	log.Printf("[%s] Starting daily neko task...", startTime.Format("2006-01-02 15:04:05"))

	results, err := fetchNekoImagesWithRetry(ctx)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no images received from API")
	}

	mediaGroup := prepareMediaGroup(results)
	if len(mediaGroup) == 0 {
		return fmt.Errorf("no valid media items prepared")
	}
	
	if err := sendMediaGroupWithRetry(ctx, b, mediaGroup); err != nil {
		return fmt.Errorf("send failed: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("[%s] Daily neko task completed successfully in %v (%d images)", 
		time.Now().Format("2006-01-02 15:04:05"), duration, len(mediaGroup))
	
	return nil
}

func setupScheduler(b *bot.Bot) (*cron.Cron, error) {
	location, err := time.LoadLocation(TIMEZONE)
	if err != nil {
		log.Printf("Warning: Failed to load timezone %s, using UTC: %v", TIMEZONE, err)
		location = time.UTC
	}

	c := cron.New(cron.WithLocation(location))
	
	_, err = c.AddFunc("0 6 * * *", func() {
		log.Println("Executing scheduled neko image sending...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := sendDailyNeko(ctx, b); err != nil {
			log.Printf("Scheduled task failed: %v", err)
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to schedule cron job: %w", err)
	}

	c.Start()
	log.Printf("Scheduled daily task at 06:00 %s", TIMEZONE)
	return c, nil
}

func setupGracefulShutdown() chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return sigChan
}

func main() {
	log.Println("Starting Telegram Neko Bot...")

	config, err := loadConfig()
	if err != nil {
		log.Fatal("Config error:", err)
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			log.Printf("Received update: %+v", update)
		}),
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Bot created successfully")

	log.Println("Sending neko images immediately for testing...")
	if err := sendDailyNeko(ctx, b); err != nil {
		log.Printf("Initial neko send failed: %v", err)
	}

	scheduler, err := setupScheduler(b)
	if err != nil {
		log.Fatal("Scheduler setup failed:", err)
	}

	go b.Start(ctx)
	log.Println("Bot started successfully")

	sigChan := setupGracefulShutdown()
	sig := <-sigChan
	
	log.Printf("Received signal %v, initiating graceful shutdown...", sig)
	
	cancel()
	if scheduler != nil {
		scheduler.Stop()
		log.Println("Scheduler stopped")
	}
	
	log.Println("Bot stopped gracefully")
}