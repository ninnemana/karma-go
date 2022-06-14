package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ninnemana/karma-go/events"

	redis "github.com/go-redis/redis/v9"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	socket, err := connect(
		os.Getenv("SLACK_APP_TOKEN"),
		os.Getenv("SLACK_BOT_TOKEN"),
	)
	if err != nil {
		log.Fatalf("failed to connect to Slack: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: ":6379",
	})
	_ = rdb.FlushDB(ctx).Err()

	handler, err := events.New(
		events.WithSocket(socket),
		events.WithRedis(rdb),
	)
	if err != nil {
		log.Fatalf("failed to create event handler: %v", err)
	}

	go func() {
		ch := make(chan os.Signal, 6)
		signal.Notify(ch,
			syscall.SIGTERM, // TERM : Exit immediatly
			syscall.SIGINT,  // INT  : Exit immediatly
			syscall.SIGQUIT, // QUIT : Exit gracefully
		)

		<-ch
		log.Println("closing socket")
		signal.Stop(ch)
		cancel()
	}()

	go func() {
		if err := handler.Receive(ctx); err != nil {
			log.Fatalf("fell out of receiving events: %v", err)
		}
	}()

	if err := socket.Run(); err != nil {
		log.Fatalf("failed to listen on socket: %v", err)
	}
}

func connect(appToken, botToken string) (*socketmode.Client, error) {
	if appToken == "" {
		return nil, errors.New("the Slack app token was not provided")
	}

	if !strings.HasPrefix(appToken, "xapp-") {
		return nil, errors.New("the provided Slack app token was not valid")
	}

	if botToken == "" {
		return nil, errors.New("the Slack bot token was not provided")
	}

	if !strings.HasPrefix(botToken, "xoxb-") {
		return nil, errors.New("the provided Slack bot token was not valid")
	}

	api := slack.New(
		botToken,
		slack.OptionDebug(true),
		slack.OptionAppLevelToken(appToken),
		slack.OptionLog(log.New(os.Stdout, "api: ", log.Lshortfile|log.LstdFlags)),
	)

	client := socketmode.New(
		api,
		//socketmode.OptionDebug(true),
		//socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	return client, nil
}
