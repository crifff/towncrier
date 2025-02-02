package main

import (
	"github.com/bwmarrin/discordgo"
	"log"
)

type Commander struct {
	Bot    *Bot
	Parent *Parent
}

func NewCommander(parent *Parent, config BotConfig) *Commander {
	return &Commander{
		Parent: parent,
		Bot:    NewBot(config),
	}
}

func (c *Commander) Open() error {
	if err := c.Bot.Open(true, false); err != nil {
		return err
	}
	p := c.Parent

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "親機を入室させる",
			Description: "発信側となる親機が指定チャンネルに参加します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel-option",
					Description: "チャンネル",
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildVoice,
					},
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "親機を退室させる",
			Description: "親機が退室します",
		},
		{
			Name:        "親機と全ての子機を退室させる",
			Description: "まとめて全てのBOTを退室させます",
		},
		{
			Name:        "子機を入室させる",
			Description: "受信側となる子機が指定チャンネルに参加します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel-option",
					Description: "チャンネル",
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildVoice,
					},
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "子機を退室させる",
			Description: "指定チャンネルにいる子機が退室します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel-option",
					Description: "チャンネル",
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildVoice,
					},
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}

	commandHandlers := map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"親機を入室させる": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			cID := extractChannelOption(i)
			if err := p.Join(i.GuildID, cID); err != nil {
				responseText(s, i, "親機が入室できませんでした")
				return
			}
			responseText(s, i, "親機が入室しました")
		},
		"親機を退室させる": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if !p.IsConnected() {
				responseText(s, i, "親機が入室していません")
				return
			}
			p.Disconnect()
			responseText(s, i, "親機が退室しました")
		},
		"親機と全ての子機を退室させる": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if p.IsConnected() {
				go p.Disconnect()
			}
			for _, child := range p.Children {
				if child.IsConnected() {
					go child.Disconnect()
				}
			}
			responseText(s, i, "全てのBOTが退室しました")
		},
		"子機を入室させる": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			cID := extractChannelOption(i)
			if p.JoinedChannel() == cID {
				responseText(s, i, "親機と同じチャンネルには入室できません")
				return
			}
			if err := p.JoinFreeChild(i.GuildID, cID); err != nil {
				responseText(s, i, "子機の入室に失敗しました")
				log.Printf("failed join child %+v\n", err)
			}

			responseText(s, i, "子機が入室しました")
		},
		"子機を退室させる": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			cID := extractChannelOption(i)
			c := joinedChild(p.Children, cID)
			if c != nil {
				c.Disconnect()
				responseText(s, i, "子機が退室しました")
			} else {
				responseText(s, i, "指定のチャンネルに子機が入室していません")
			}

		},
	}

	if _, err := c.Bot.session.ApplicationCommandBulkOverwrite(c.Bot.session.State.User.ID, GuildID, commands); err != nil {
		return err
	}

	c.Bot.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	return nil
}
