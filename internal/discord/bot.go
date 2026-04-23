package discord

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/carlosmaranje/mango/internal/llm"
	"github.com/carlosmaranje/mango/internal/orchestrator"
)

const (
	taskPollMaxRetries    = 300
	typingRefreshInterval = 8 * time.Second
)

type Bot struct {
	session    *discordgo.Session
	router     *Router
	history    *ChannelHistory
	dispatcher *orchestrator.Dispatcher
	global     bool

	mu         sync.Mutex
	processing map[string]bool // message ID dedup
}

func NewBot(token string, router *Router, history *ChannelHistory, dispatcher *orchestrator.Dispatcher, global bool) (*Bot, error) {
	sess, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	sess.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent | discordgo.IntentsGuildPresences

	b := &Bot{
		session:    sess,
		router:     router,
		history:    history,
		dispatcher: dispatcher,
		global:     global,
		processing: make(map[string]bool),
	}
	sess.AddHandler(b.onMessage)
	sess.AddHandler(b.onReady)
	return b, nil
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("discord: bot ready as %s#%s", s.State.User.Username, s.State.User.Discriminator)
	for cid, agent := range b.router.Bindings() {
		log.Printf("discord: active binding: channel %s -> agent %q", cid, agent)
	}

	if err := s.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status: "online",
		Activities: []*discordgo.Activity{
			{
				Name: "for tasks",
				Type: discordgo.ActivityTypeWatching,
			},
		},
	}); err != nil {
		log.Printf("discord: update status: %v", err)
	}
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = b.session.Close()
	}()
	return nil
}

func (b *Bot) Close() error {
	return b.session.Close()
}

func (b *Bot) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if s.State.User != nil && m.Author.ID == s.State.User.ID {
		return
	}
	if m.Author.Bot {
		return
	}

	// Deduplicate: skip if we're already processing this message
	b.mu.Lock()
	if b.processing[m.ID] {
		b.mu.Unlock()
		return
	}
	b.processing[m.ID] = true
	b.mu.Unlock()

	// Clean up old entries periodically (keep map from growing unbounded)
	go func() {
		time.Sleep(5 * time.Minute)
		b.mu.Lock()
		delete(b.processing, m.ID)
		b.mu.Unlock()
	}()

	isDM := m.GuildID == ""
	isMentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			isMentioned = true
			break
		}
	}

	agentName := b.router.Resolve(m.ChannelID)
	if agentName == "" {
		if !isDM && !isMentioned && !b.global {
			return
		}

		if isDM {
			log.Printf("discord: message from %s in DM (falling back to orchestrator)", m.Author.Username)
		} else if isMentioned {
			log.Printf("discord: message from %s in channel %s (mentioned, falling back to orchestrator)", m.Author.Username, m.ChannelID)
		} else {
			log.Printf("discord: message from %s in channel %s (global mode, falling back to orchestrator)", m.Author.Username, m.ChannelID)
		}
	} else {
		log.Printf("discord: message from %s in channel %s -> routed to agent %q", m.Author.Username, m.ChannelID, agentName)
	}

	content := m.Content
	if isMentioned {
		mention1 := fmt.Sprintf("<@%s>", s.State.User.ID)
		mention2 := fmt.Sprintf("<@!%s>", s.State.User.ID)
		content = strings.ReplaceAll(content, mention1, "")
		content = strings.ReplaceAll(content, mention2, "")
		content = strings.TrimSpace(content)
	}

	priorHistory := b.history.Get(m.ChannelID)
	b.history.Append(m.ChannelID, llm.Message{Role: "user", Content: content})

	ctx := context.Background()
	task, err := b.dispatcher.SubmitWithHistory(ctx, content, agentName, priorHistory)
	if err != nil {
		log.Printf("discord: dispatch: %v", err)
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "error: "+err.Error(), m.Reference())
		return
	}

	log.Printf("discord: task %s submitted for %s", task.ID, m.Author.Username)

	typingDone := make(chan struct{})
	go b.keepTyping(s, m.ChannelID, typingDone)
	go b.waitAndReply(s, m, task.ID, typingDone)
}

func (b *Bot) keepTyping(s *discordgo.Session, channelID string, done <-chan struct{}) {
	if err := s.ChannelTyping(channelID); err != nil {
		log.Printf("discord: typing: %v", err)
	}
	ticker := time.NewTicker(typingRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := s.ChannelTyping(channelID); err != nil {
				log.Printf("discord: typing: %v", err)
				return
			}
		}
	}
}

func (b *Bot) waitAndReply(s *discordgo.Session, m *discordgo.MessageCreate, taskID string, typingDone chan<- struct{}) {
	defer close(typingDone)
	for range taskPollMaxRetries {
		t, ok := b.dispatcher.Get(taskID)
		if !ok {
			return
		}
		if t.Status == orchestrator.StatusDone || t.Status == orchestrator.StatusFailed {
			reply := t.Result
			if t.Status == orchestrator.StatusFailed {
				reply = "error: " + t.Error
			}
			b.history.Append(m.ChannelID, llm.Message{Role: "assistant", Content: reply})
			if _, err := s.ChannelMessageSendReply(m.ChannelID, reply, m.Reference()); err != nil {
				log.Printf("discord: send reply: %v", err)
			}
			return
		}
		time.Sleep(time.Second)
	}
}
