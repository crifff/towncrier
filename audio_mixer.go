package main

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

// AudioStream は単一ユーザーからの音声ストリームを表します
type AudioStream struct {
	SSRC             uint32
	UserID           string
	LastActive       time.Time
	OpusData         []byte  // 最後に受信したOpusデータ
	PCMData          []int16 // デコードされたPCMデータ
	IsActive         bool    // 音声アクティビティ状態
	VolumeMultiplier float32 // 音量調整係数（1.0がデフォルト）
}

// AudioMixer は複数の音声ストリームをミキシングする機能を提供します
type AudioMixer struct {
	streams     map[uint32]*AudioStream
	mu          sync.Mutex
	frameSize   int
	channels    int
	sampleRate  int
	mixedPCM    []int16
	decoder     *opus.Decoder
	encoder     *opus.Encoder
	speakerSSRC uint32 // 現在のスピーカーのSSRC（優先制御用）
}

// NewAudioMixer は新しいAudioMixerインスタンスを作成します
func NewAudioMixer() (*AudioMixer, error) {
	// Discordの音声仕様に合わせたパラメータ
	sampleRate := 48000 // 48kHz
	channels := 2       // ステレオ
	frameSize := 960    // 20msのフレームサイズ (48000 * 0.02)

	// デコーダーの初期化
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}

	// エンコーダーの初期化
	encoder, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, err
	}

	// ビットレートを設定（64kbps）
	if err := encoder.SetBitrate(64000); err != nil {
		return nil, err
	}

	return &AudioMixer{
		streams:    make(map[uint32]*AudioStream),
		frameSize:  frameSize,
		channels:   channels,
		sampleRate: sampleRate,
		mixedPCM:   make([]int16, frameSize*channels),
		decoder:    decoder,
		encoder:    encoder,
	}, nil
}

// AddPacket は新しい音声パケットをミキサーに追加します
func (m *AudioMixer) AddPacket(packet *discordgo.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// ストリームが存在しない場合は新規作成
	stream, exists := m.streams[packet.SSRC]
	if !exists {
		stream = &AudioStream{
			SSRC:             packet.SSRC,
			OpusData:         make([]byte, 0, 1000),
			PCMData:          make([]int16, m.frameSize*m.channels),
			LastActive:       time.Now(),
			VolumeMultiplier: 1.0, // デフォルトの音量
		}
		m.streams[packet.SSRC] = stream
		log.Printf("新しい音声ストリームを検出: SSRC=%d", packet.SSRC)
	}

	// Opusデータを保存
	stream.OpusData = make([]byte, len(packet.Opus))
	copy(stream.OpusData, packet.Opus)

	// Opusデータをデコード
	pcmBuffer := make([]int16, m.frameSize*m.channels)
	samplesPerChannel, err := m.decoder.Decode(packet.Opus, pcmBuffer)
	if err != nil {
		return err
	}

	// デコードされたPCMデータを保存
	actualSamples := samplesPerChannel * m.channels
	if actualSamples > 0 {
		stream.PCMData = pcmBuffer[:actualSamples]
	}

	// 音声アクティビティを検出
	stream.IsActive = m.isAudioActive(stream.PCMData, 1000) // しきい値は調整が必要
	stream.LastActive = time.Now()

	// スピーカーが設定されていない場合は、このストリームをスピーカーとして設定
	if m.speakerSSRC == 0 {
		m.speakerSSRC = packet.SSRC
		log.Printf("スピーカーを設定: SSRC=%d", packet.SSRC)
	}

	return nil
}

// isAudioActive は音声データがアクティブ（無音でない）かを判定します
func (m *AudioMixer) isAudioActive(pcmData []int16, threshold int) bool {
	if len(pcmData) == 0 {
		return false
	}

	// RMSエネルギーを計算
	var sum int64
	for _, sample := range pcmData {
		sum += int64(sample) * int64(sample)
	}
	rms := int(math.Sqrt(float64(sum) / float64(len(pcmData))))

	return rms > threshold
}

// GetMixedPacket は現在アクティブなすべてのストリームをミキシングしたパケットを返します
func (m *AudioMixer) GetMixedPacket() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 古いストリームをクリーンアップ（5秒以上非アクティブ）
	now := time.Now()
	for ssrc, stream := range m.streams {
		if now.Sub(stream.LastActive) > 5*time.Second {
			delete(m.streams, ssrc)
			log.Printf("非アクティブなストリームを削除: SSRC=%d", ssrc)

			// スピーカーが削除された場合、スピーカーをリセット
			if ssrc == m.speakerSSRC {
				m.speakerSSRC = 0
			}
		}
	}

	// アクティブなストリームがなければ無音を返す
	if len(m.streams) == 0 {
		return make([]byte, 0), nil
	}

	// ミキシングバッファをクリア
	for i := range m.mixedPCM {
		m.mixedPCM[i] = 0
	}

	// すべてのストリームをミキシング（加算）
	activeStreams := 0
	for _, stream := range m.streams {
		if len(stream.PCMData) == 0 || !stream.IsActive {
			continue
		}

		activeStreams++
		// PCMサンプルを加算（音量調整を適用）
		for i := 0; i < len(stream.PCMData) && i < len(m.mixedPCM); i++ {
			// 音量調整を適用
			adjustedSample := int32(float32(stream.PCMData[i]) * stream.VolumeMultiplier)
			// クリッピング防止
			if adjustedSample > 32767 {
				adjustedSample = 32767
			} else if adjustedSample < -32768 {
				adjustedSample = -32768
			}
			m.mixedPCM[i] += int16(adjustedSample)
		}
	}

	// ボリュームを正規化（クリッピング防止）
	if activeStreams > 1 {
		for i := range m.mixedPCM {
			// 単純な正規化
			normalizedSample := float32(m.mixedPCM[i]) / float32(activeStreams)
			m.mixedPCM[i] = int16(normalizedSample)
		}
	}

	// ミキシングしたPCMデータをOpusにエンコード
	opusData := make([]byte, 1000) // 十分なバッファサイズ
	n, err := m.encoder.Encode(m.mixedPCM, opusData)
	if err != nil {
		return nil, err
	}

	return opusData[:n], nil
}

// SetSpeaker は特定のSSRCをスピーカーとして設定します
func (m *AudioMixer) SetSpeaker(ssrc uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.streams[ssrc]; exists {
		m.speakerSSRC = ssrc
		log.Printf("スピーカーを手動設定: SSRC=%d", ssrc)
	}
}

// GetCurrentSpeaker は現在のスピーカーのSSRCを返します
func (m *AudioMixer) GetCurrentSpeaker() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.speakerSSRC
}

// Close はAudioMixerのリソースを解放します
func (m *AudioMixer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// エンコーダーとデコーダーを解放
	if m.encoder != nil {
		m.encoder.Close()
	}

	if m.decoder != nil {
		m.decoder.Close()
	}

	m.streams = make(map[uint32]*AudioStream)
}

// SetVolume は特定のSSRCの音量を設定します
func (m *AudioMixer) SetVolume(ssrc uint32, volume float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stream, exists := m.streams[ssrc]; exists {
		// 音量を0.0〜2.0の範囲に制限（0=無音、1.0=通常、2.0=2倍）
		if volume < 0.0 {
			volume = 0.0
		} else if volume > 2.0 {
			volume = 2.0
		}

		stream.VolumeMultiplier = volume
		log.Printf("ストリーム SSRC=%d の音量を %.1f に設定", ssrc, volume)
	}
}
