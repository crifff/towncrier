package main

import (
	"errors"
	"github.com/bwmarrin/discordgo"
	"github.com/pion/rtp"
	"log"
	"sync"
)

type Parent struct {
	bot      *Bot
	Children []*Child
	chList   []chan []byte
	mu       sync.Mutex
}

func (p *Parent) Open() error {
	if err := p.bot.Open(true, false); err != nil {
		return err
	}

	return nil
}

func extractChannelOption(i *discordgo.InteractionCreate) string {
	options := i.ApplicationCommandData().Options
	// Or convert the slice into a map
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	v := optionMap["channel-option"].Value.(string)
	return v
}

func responseText(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	log.Printf("%+v\n", err)
	return
}

func joinedChild(childrenPool []*Child, cID string) *Child {
	for _, child := range childrenPool {
		if child == nil {
			panic("child is nil")
		}
		if child.Bot.Conn == nil {
			continue
		}
		if child.Bot.Conn.ChannelID == cID {
			return child
		}
	}
	return nil
}

//func freeChild(childrenPool []*Child) (*Child, error) {
//	for _, child := range childrenPool {
//		if child == nil {
//			panic("child is nil")
//		}
//		if child.Bot.Conn == nil {
//			return child, nil
//		}
//	}
//	return nil, errors.New("full")
//
//}

func (p *Parent) AddChild(child *Child) {
	p.mu.Lock()
	p.Children = append(p.Children, child)
	p.mu.Unlock()
}

func (p *Parent) FreeChild() (*Child, error) {
	for _, c := range p.Children {
		if c == nil {
			panic("child is nil")
		}
		if !c.IsConnected() {
			return c, nil
		}
	}
	return nil, errors.New("full")
}

func (p *Parent) JoinFreieChild(gID, cID string) error {
	for i := range p.Children {
		if p.Children[i] == nil {
			panic("child is nil")
		}
		if !p.Children[i].IsConnected() {
			return p.Children[i].Join(gID, cID)
		}
	}
	return nil
}

func createPionRTPPacket(p *discordgo.Packet) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version: 2,
			// Taken from Discord voice docs
			PayloadType:    0x78,
			SequenceNumber: p.Sequence,
			Timestamp:      p.Timestamp,
			SSRC:           p.SSRC,
		},
		Payload: p.Opus,
	}
}

func (p *Parent) Handle() {
	for r := range p.bot.Conn.OpusRecv {
		pa := createPionRTPPacket(r)
		for _, child := range p.Children {
			if child.Bot.Conn == nil {
				continue
			}
			go func(ch chan []byte, data []byte) {
				if child.Bot.Conn == nil {
					return
				}
				ch <- data
			}(child.Bot.Conn.OpusSend, pa.Payload)
		}
	}
}

func (p *Parent) Disconnect() {
	p.bot.Conn.Disconnect()
	p.bot.Conn = nil
}

func (p *Parent) Close() {
	var wg sync.WaitGroup
	for i := range p.Children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Children[i].Close()
		}()
	}
	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		//cmds, err := p.bot.session.ApplicationCommands(p.bot.session.State.User.ID, GuildID)
		//if err != nil {
		//	panic(err)
		//}
		//for _, cmd := range cmds {
		//	if err := p.bot.session.ApplicationCommandDelete(p.bot.session.State.User.ID, GuildID, cmd.ID); err != nil {
		//		panic(err)
		//	}
		//}
	}()
	wg.Wait()

	p.bot.Close()
	log.Println("親機を終了しました")
}

func (p *Parent) Join(gID, cID string) error {
	if err := p.bot.JoinChannel(gID, cID, true, false); err != nil {
		return err
	}
	go p.Handle()
	return nil
}

func (p *Parent) IsConnected() bool {
	return p.bot.Conn != nil
}

func (p *Parent) JoinedChannel() string {
	if p.IsConnected() {
		return p.bot.Conn.ChannelID
	}
	return ""
}

func NewParent(config BotConfig) *Parent {
	return &Parent{
		bot: NewBot(config),
	}
}
