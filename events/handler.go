package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	redis "github.com/go-redis/redis/v9"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Handler struct {
	socket *socketmode.Client
	redis  *redis.Client
}

type Option func(*Handler) error

func WithSocket(client *socketmode.Client) Option {
	return func(h *Handler) error {
		if _, err := client.AuthTest(); err != nil {
			return fmt.Errorf("failed to ensure the client is authenticated: %w", err)
		}

		h.socket = client

		return nil
	}
}

func WithRedis(client *redis.Client) Option {
	return func(h *Handler) error {
		if err := client.Ping(context.Background()).Err(); err != nil {
			return fmt.Errorf("failed to ensure the redis client is connected: %w", err)
		}

		h.redis = client

		return nil
	}
}

func New(options ...Option) (*Handler, error) {
	var h Handler
	for _, opt := range options {
		if err := opt(&h); err != nil {
			return nil, fmt.Errorf("failed to handle option: %w", err)
		}
	}

	return &h, nil
}

func (h *Handler) Receive(ctx context.Context) error {
	for {
		select {
		case event := <-h.socket.Events:
			karma, err := handleEvent(event)
			if err != nil {
				log.Printf("failed to handle event: %v", err)
				continue
			}

			if karma == nil {
				continue
			}

			log.Println(karma.UserID, karma.Change)
			if err := h.redis.IncrBy(ctx, karma.UserID, karma.Change).Err(); err != nil {
				log.Printf("failed to adjust user's '%s' karma: %v\n", karma.UserID, err)
				continue
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func handleEvent(evt socketmode.Event) (*Karma, error) {
	data, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		// not an event we care about
		return nil, nil
	}

	if data.InnerEvent.Type != slack.TYPE_MESSAGE {
		// not a type we care about
		return nil, nil
	}

	inner, ok := data.Data.(*slackevents.EventsAPICallbackEvent)
	if !ok || inner.InnerEvent == nil {
		return nil, fmt.Errorf(
			"provded data '%T' was not the expected type '%T'",
			data.Data,
			&slackevents.EventsAPICallbackEvent{},
		)
	}

	var msg slack.Msg
	if err := json.Unmarshal(*inner.InnerEvent, &msg); err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	var (
		mentionedUser string
		text          string
	)

	for _, block := range msg.Blocks.BlockSet {
		if block.BlockType() != slack.MBTRichText {
			continue
		}

		richText, ok := block.(*slack.RichTextBlock)
		if !ok || richText == nil {
			continue
		}

		for _, element := range richText.Elements {
			if element.RichTextElementType() != slack.RTESection {
				continue
			}

			section, ok := element.(*slack.RichTextSection)
			if !ok || section == nil {
				continue
			}

			for _, sectionEl := range section.Elements {
				switch sectionEl.RichTextSectionElementType() {
				case slack.RTSEUser:
					e := sectionEl.(*slack.RichTextSectionUserElement)
					if e == nil {
						continue
					}

					mentionedUser = e.UserID
				case slack.RTSEText:
					e := sectionEl.(*slack.RichTextSectionTextElement)
					if e == nil {
						continue
					}

					text = strings.TrimSpace(e.Text)
				}
			}
		}
	}

	if mentionedUser == "" || text == "" {
		return nil, nil
	}

	karma := &Karma{
		UserID: mentionedUser,
	}

	switch {
	case strings.HasPrefix(text, "++"):
		karma.Change = 1
	case strings.HasPrefix(text, "--"):
		karma.Change = -1
	}

	return karma, nil
}

type Karma struct {
	UserID string
	Change int64
}
