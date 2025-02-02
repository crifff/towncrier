package main

import (
	"log"
)

type Child struct {
	Num int
	Bot *Bot
}

func NewChild(config BotConfig, num int) *Child {
	b := NewBot(config)
	return &Child{
		Num: num,
		Bot: b,
	}
}

func (c *Child) Open() error {
	return c.Bot.Open(false, true)
}

func (c *Child) Close() error {
	log.Printf("子機%02dを終了しました", c.Num)
	return c.Bot.Close()
}

func (c *Child) Send(payload []byte) {
	c.Bot.Conn.OpusSend <- payload
}

func (c *Child) Join(gID, cID string) error {
	if err := c.Bot.JoinChannel(gID, cID, false, true); err != nil {
		return err
	}
	return nil
}

func (c *Child) Disconnect() {
	c.Bot.Conn.Disconnect()
	c.Bot.Conn = nil

}

func (c *Child) IsConnected() bool {
	return c.Bot.Conn != nil
}
