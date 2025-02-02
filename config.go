package main

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	Commander BotConfig
	Parent    BotConfig
	Children  []BotConfig
}

type BotConfig struct {
	Token string
}

func LoadFile() ([]string, error) {
	filename := "tokens.txt"
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, nil
}

func NewConfig(lines []string) (*Config, error) {
	config := &Config{}
	config.Commander = BotConfig{Token: lines[0]}
	config.Parent = BotConfig{Token: lines[1]}

	for _, line := range lines[2:] {
		l := strings.TrimSpace(line)
		config.Children = append(config.Children, BotConfig{Token: l})
	}

	return config, nil
}
