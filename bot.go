package main

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	session *discordgo.Session
	config  BotConfig
	Conn    *discordgo.VoiceConnection
}

func NewBot(config BotConfig) *Bot {
	bot := &Bot{
		config: config,
	}

	s, err := discordgo.New("Bot " + config.Token)
	if err != nil {
		panic(err)
	}
	s.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates)

	bot.session = s
	return bot
}

func (bot *Bot) Close() error {
	if bot.Conn != nil {
		if bot.Conn.OpusRecv != nil {
			close(bot.Conn.OpusRecv)
		}
		bot.Conn.Close()
		bot.Conn = nil
	}
	if bot.session != nil {
		bot.session.Close()
	}
	return nil
}

func (bot *Bot) Open(isMute, isDeafen bool) error {
	err := bot.session.Open()
	if err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	return nil
}

func (bot *Bot) JoinChannel(gID, cID string, mute, deaf bool) error {
	v, err := bot.session.ChannelVoiceJoin(gID, cID, mute, deaf)
	if err != nil {
		return fmt.Errorf("error joining voice channel: %w", err)
	}
	bot.Conn = v
	return nil
}

func (bot *Bot) Handle() {

}
