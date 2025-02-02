package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var GuildID = ""

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) != 2 {
		fmt.Println("引数を指定してください")
		return
	}

	arg := os.Args[1]
	if _, err := url.ParseRequestURI(arg); err != nil {
		fmt.Println("チャンネルの招待URLを指定してください")
		return
	}
	s, err := getGuildIDFromInviteLink(config.Parent.Token, arg)
	if err != nil {
		log.Fatal(err)
	}
	GuildID = s

	parent := NewParent(config.Parent)
	if err := parent.Open(); err != nil {
		panic(err)
	}
	//parent.Join(GuildID, "1333719217098326039")
	log.Println("親機が起動しました")
	defer func() {
		if err := recover(); err != nil {
			parent.Close()
		}
	}()

	var wg sync.WaitGroup
	for i, c := range config.Children {
		if c.Token == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := NewChild(c, i+1)
			if err := b.Open(); err != nil {
				panic(err)
			}
			parent.AddChild(b)
			//b.Join(GuildID, "1334306012043415612")
			log.Printf("子機%02dが起動しました", i+1)
		}()
	}
	wg.Wait()

	c := NewCommander(parent, config.Commander)
	if err := c.Open(); err != nil {
		panic(err)
	}
	log.Printf("コマンダーが起動しました")

	fmt.Println("終了するにはctrl+cを押してください")

	idleClosed := make(chan struct{})
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		log.Println("処理を中断しました")
		parent.Close()
		close(idleClosed)
	}()
	<-idleClosed
}

func loadConfig() (*Config, error) {
	lines, err := LoadFile()
	if err != nil {
		log.Fatal(err)
	}
	config, err := NewConfig(lines)
	if err != nil {
		log.Fatal(err)
	}
	return config, nil
}
