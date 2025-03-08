package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pion/rtp"
)

type Parent struct {
	bot        *Bot
	Children   []*Child
	chList     []chan []byte
	mu         sync.Mutex
	audioMixer *AudioMixer
	stopCh     chan struct{}
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

func (p *Parent) JoinFreeChild(gID, cID string) error {
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
	// 受信パケットをミキサーに追加
	go func() {
		for r := range p.bot.Conn.OpusRecv {
			p.audioMixer.AddPacket(r)
		}
	}()

	// 定期的にミキシングして送信（20ms間隔）
	ticker := time.NewTicker(20 * time.Millisecond)
	go func() {
		for {
			select {
			case <-ticker.C:
				// ミキシングしたデータを取得
				mixedData, err := p.audioMixer.GetMixedPacket()
				if err != nil {
					log.Printf("ミキシングエラー: %v", err)
					continue
				}
				if len(mixedData) == 0 {
					continue
				}

				// ミキシングしたデータを全ての子機に送信
				for _, child := range p.Children {
					if child.Bot.Conn == nil {
						continue
					}

					// データのコピーを作成して送信
					dataCopy := make([]byte, len(mixedData))
					copy(dataCopy, mixedData)

					go func(ch chan []byte, data []byte) {
						if ch != nil {
							ch <- data
						}
					}(child.Bot.Conn.OpusSend, dataCopy)
				}
			case <-p.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (p *Parent) Disconnect() {
	p.bot.Conn.Disconnect()
	p.bot.Conn = nil
}

func (p *Parent) Close() {
	// ミキシングを停止
	close(p.stopCh)

	// 子機を終了
	var wg sync.WaitGroup
	for i := range p.Children {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p.Children[idx].Close()
		}(i)
	}
	wg.Wait()

	// オーディオミキサーを終了
	if p.audioMixer != nil {
		p.audioMixer.Close()
	}

	p.bot.Close()
	log.Println("親機を終了しました")
}

func (p *Parent) Join(gID, cID string) error {
	if err := p.bot.JoinChannel(gID, cID, true, false); err != nil {
		return err
	}

	userMuteStates = make(map[string]userMuteState)
	// VoiceStateUpdate イベントのハンドラを登録
	p.bot.session.AddHandler(voiceStateUpdate)

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
	mixer, err := NewAudioMixer()
	if err != nil {
		log.Printf("音声ミキサーの初期化エラー: %v", err)
		// エラーが発生した場合でも、nilを返さずに空のミキサーを作成
		mixer = &AudioMixer{
			streams:   make(map[uint32]*AudioStream),
			frameSize: 960,
		}
	}

	return &Parent{
		bot:        NewBot(config),
		audioMixer: mixer,
		stopCh:     make(chan struct{}),
	}
}

// 注: success.mp3ファイルは使用していないため、embedディレクティブを削除

type userMuteState struct {
	history []time.Time
}

const (
	historySize   = 5
	muteThreshold = 2 * time.Second
	taps          = 4
	sampleRate    = 48000
	channels      = 2
	frameSizeMs   = 20
)

var (
	userMuteStates map[string]userMuteState
	speaker        string            // 最後にメッセージを送信したユーザーの ID
	speakerSSRC    uint32            // 最後にメッセージを送信したユーザーの SSRC
	ssrcToUserID   map[uint32]string //voiceStateUpdateで更新するmap
)

func voiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	// VoiceStateUpdateにはSSRCが含まれていないため、マッピングが必要
	userID := vs.UserID
	currentState := vs.Mute

	state, ok := userMuteStates[userID]
	if !ok {
		userMuteStates[userID] = userMuteState{history: []time.Time{}}
		state = userMuteStates[userID]
	}

	if len(state.history) > 0 {
		previousState := !currentState
		if previousState == currentState {
			return
		}
	}

	state.history = append(state.history, time.Now())

	if len(state.history) > historySize {
		state.history = state.history[len(state.history)-historySize:]
	}

	if len(state.history) >= taps {
		duration := state.history[len(state.history)-1].Sub(state.history[len(state.history)-taps])
		if duration < muteThreshold {
			fmt.Printf("User %s toggled mute %d times within %v!\n", userID, taps, muteThreshold)

			vc := s.VoiceConnections[vs.GuildID]
			if vc == nil || vc.ChannelID != vs.ChannelID {
				fmt.Println("Not in the same voice channel")
				return
			}
			// speaker が現在のユーザーと異なる場合のみメッセージを送信
			if speaker != userID {
				fmt.Printf("User %s toggled mute %d times within %v!\n", userID, taps, muteThreshold)
				sendMuteToggleMessage(s, vs.GuildID, vs.ChannelID, userID)
				speaker = userID // speaker を更新

				// 親機のオーディオミキサーにスピーカー情報を設定
				// 注: 実際のSSRCはここでは取得できないため、別の方法で関連付ける必要がある
			}
			state.history = []time.Time{}
		}
	}

	userMuteStates[userID] = state

}

// ボイスチャンネルに紐づくテキストチャンネルにメッセージを送信する関数
func sendMuteToggleMessage(s *discordgo.Session, guildID, voiceChannelID, userID string) {

	// ボイスチャンネルの情報を取得
	voiceChannel, err := s.Channel(voiceChannelID)
	if err != nil {
		fmt.Println("Error getting voice channel:", err)
		return
	}

	// ボイスチャンネルと同じ名前のテキストチャンネルを探す
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		fmt.Println("Error getting guild channels:", err)
		return
	}

	var textChannelID string
	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildText && channel.Name == voiceChannel.Name {
			textChannelID = channel.ID
			break
		}
	}

	if textChannelID == "" {
		fmt.Println("No corresponding text channel found.")
		// 必要であれば、ここで代替のチャンネルに送信する処理などを記述
		return
	}

	// ユーザー情報を取得（メンション用）
	user, err := s.User(userID)
	if err != nil {
		fmt.Println("Error getting user:", err)
		return // ユーザー情報が取得できなくても、とりあえずメッセージは送る
	}

	// メッセージを送信
	message := fmt.Sprintf("<@%s> スピーカーに登録しました", user.ID) // メンション形式
	// message := fmt.Sprintf("User %s#%s toggled mute within 1 second in voice channel %s!", user.Username, user.Discriminator, voiceChannel.Name) // 通常のユーザー名形式
	_, err = s.ChannelMessageSend(textChannelID, message)
	if err != nil {
		fmt.Println("Error sending message:", err)
	}
}
