package main

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"net/url"
	"regexp"
	"strings"
)

func getGuildIDFromInviteLink(token, inviteLink string) (string, error) {
	u, err := url.Parse(inviteLink)
	if err != nil {
		return "", fmt.Errorf("invalid invite link: %w", err)
	}

	// discord.gg/<code> or discord.com/invite/<code> の形式に対応
	pathParts := strings.Split(u.Path, "/")
	inviteCode := ""
	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] != "" {
			inviteCode = pathParts[i]
			break
		}
	}

	if inviteCode == "" {
		return "", fmt.Errorf("invite code not found in link")
	}

	// 正規表現でinviteCodeが有効な文字列か確認 (必要に応じて)
	if !isValidInviteCode(inviteCode) {
		return "", fmt.Errorf("invalid invite code format")
	}

	// Discord APIを使ってGuild IDを取得
	dg, err := discordgo.New(token) // Botトークンは不要
	if err != nil {
		return "", fmt.Errorf("failed to create Discord session: %w", err)
	}
	defer dg.Close()

	invite, err := dg.Invite(inviteCode)
	if err != nil {
		return "", fmt.Errorf("failed to get invite: %w", err)
	}

	if invite.Guild == nil {
		return "", fmt.Errorf("invite Guild is nil, check invite code and bot permissions")
	}

	return invite.Guild.ID, nil
}

func isValidInviteCode(code string) bool {
	// 有効な招待コードの形式を正規表現でチェック
	// 例: 英数字とハイフンのみ、長さなど
	match, _ := regexp.MatchString(`^[a-zA-Z0-9-]+$`, code) // 例: 英数字とハイフンのみ
	return match
}
