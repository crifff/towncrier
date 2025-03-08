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
	OpusData         []byte        // 最後に受信したOpusデータ
	PCMData          []int16       // デコードされたPCMデータ
	IsActive         bool          // 音声アクティビティ状態
	VolumeMultiplier float32       // 音量調整係数（1.0がデフォルト）
	lastActiveTime   time.Time     // 最後にアクティブだった時間
	holdTime         time.Duration // 無音後も音声として扱う時間
	sequence         uint16        // パケットのシーケンス番号
}

// AudioMixer は複数の音声ストリームをミキシングする機能を提供します
type AudioMixer struct {
	streams          map[uint32]*AudioStream
	mu               sync.Mutex
	frameSize        int
	channels         int
	sampleRate       int
	mixedPCM         []int16
	decoder          *opus.Decoder
	encoder          *opus.Encoder
	speakerSSRC      uint32              // 現在のスピーカーのSSRC（優先制御用）
	jitterBuffer     map[uint32][][]byte // SSRCごとのジッターバッファ
	jitterBufferSize int                 // バッファサイズ（フレーム数）
	zeroBuffer       []int16             // 0で初期化されたバッファ
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

	// パケットロス対策のためのFEC（前方誤り訂正）を有効化
	if err := encoder.SetInbandFEC(true); err != nil {
		log.Printf("FEC設定エラー: %v", err)
	}

	// 10%のパケットロスを想定
	if err := encoder.SetPacketLossPerc(10); err != nil {
		log.Printf("パケットロス設定エラー: %v", err)
	}

	// 0で初期化されたバッファを作成
	zeroBuffer := make([]int16, frameSize*channels)

	return &AudioMixer{
		streams:          make(map[uint32]*AudioStream),
		frameSize:        frameSize,
		channels:         channels,
		sampleRate:       sampleRate,
		mixedPCM:         make([]int16, frameSize*channels),
		decoder:          decoder,
		encoder:          encoder,
		jitterBuffer:     make(map[uint32][][]byte),
		jitterBufferSize: 3, // 3フレーム（約60ms）のバッファ
		zeroBuffer:       zeroBuffer,
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
			lastActiveTime:   time.Now(),
			holdTime:         200 * time.Millisecond, // 200msのホールドタイム
			VolumeMultiplier: 1.0,                    // デフォルトの音量
			sequence:         packet.Sequence,
		}
		m.streams[packet.SSRC] = stream
		log.Printf("新しい音声ストリームを検出: SSRC=%d", packet.SSRC)
	}

	// ジッターバッファにパケットを追加
	if _, exists := m.jitterBuffer[packet.SSRC]; !exists {
		m.jitterBuffer[packet.SSRC] = make([][]byte, 0, m.jitterBufferSize)
	}

	// パケットのコピーを作成
	opusData := make([]byte, len(packet.Opus))
	copy(opusData, packet.Opus)

	// バッファに追加
	m.jitterBuffer[packet.SSRC] = append(m.jitterBuffer[packet.SSRC], opusData)

	// シーケンス番号を更新
	stream.sequence = packet.Sequence

	// バッファサイズを制限
	if len(m.jitterBuffer[packet.SSRC]) > m.jitterBufferSize {
		// 最も古いパケットを処理
		oldestPacket := m.jitterBuffer[packet.SSRC][0]
		m.jitterBuffer[packet.SSRC] = m.jitterBuffer[packet.SSRC][1:]

		// Opusデータを保存
		stream.OpusData = make([]byte, len(oldestPacket))
		copy(stream.OpusData, oldestPacket)

		// Opusデータをデコード
		pcmBuffer := make([]int16, m.frameSize*m.channels)
		samplesPerChannel, err := m.decoder.Decode(oldestPacket, pcmBuffer)
		if err != nil {
			return err
		}

		// デコードされたPCMデータを保存
		actualSamples := samplesPerChannel * m.channels
		if actualSamples > 0 {
			stream.PCMData = pcmBuffer[:actualSamples]
		}

		// 音声アクティビティを検出
		stream.IsActive = m.isAudioActive(stream, stream.PCMData, 800) // しきい値を800に下げる
		stream.LastActive = time.Now()
	}

	// スピーカーが設定されていない場合は、このストリームをスピーカーとして設定
	if m.speakerSSRC == 0 {
		m.speakerSSRC = packet.SSRC
		log.Printf("スピーカーを設定: SSRC=%d", packet.SSRC)
	}

	return nil
}

// isAudioActive は音声データがアクティブ（無音でない）かを判定します
func (m *AudioMixer) isAudioActive(stream *AudioStream, pcmData []int16, threshold int) bool {
	if len(pcmData) == 0 {
		return false
	}

	// RMSエネルギーを計算
	var sum int64
	for _, sample := range pcmData {
		sum += int64(sample) * int64(sample)
	}
	rms := int(math.Sqrt(float64(sum) / float64(len(pcmData))))

	// 現在の時刻
	now := time.Now()

	// 音声がアクティブか判定
	isActive := rms > threshold

	// アクティブな場合、最終アクティブ時間を更新
	if isActive {
		stream.lastActiveTime = now
		return true
	}

	// ホールドタイム内なら、まだアクティブとみなす
	if now.Sub(stream.lastActiveTime) < stream.holdTime {
		return true
	}

	return false
}

// GetMixedPacket は現在アクティブなすべてのストリームをミキシングしたパケットを返します
func (m *AudioMixer) GetMixedPacket() ([]byte, error) {
	m.mu.Lock()

	// 処理対象のストリームをコピー（ロックを早く解放するため）
	activeStreams := make([]*AudioStream, 0, len(m.streams))

	// 古いストリームをクリーンアップ（5秒以上非アクティブ）
	now := time.Now()
	for ssrc, stream := range m.streams {
		if now.Sub(stream.LastActive) > 5*time.Second {
			delete(m.streams, ssrc)
			delete(m.jitterBuffer, ssrc)
			log.Printf("非アクティブなストリームを削除: SSRC=%d", ssrc)

			// スピーカーが削除された場合、スピーカーをリセット
			if ssrc == m.speakerSSRC {
				m.speakerSSRC = 0
			}
		} else if len(stream.PCMData) > 0 && stream.IsActive {
			// アクティブなストリームを追加
			activeStreams = append(activeStreams, stream)
		}
	}

	// アクティブなストリームがなければ無音を返す
	if len(activeStreams) == 0 {
		m.mu.Unlock() // 早めにロックを解放
		return make([]byte, 0), nil
	}

	// ミキシングバッファをクリア（高速化版）
	copy(m.mixedPCM, m.zeroBuffer)

	m.mu.Unlock() // 早めにロックを解放

	// すべてのストリームをミキシング（加算）
	streamCount := 0
	for _, stream := range activeStreams {
		streamCount++
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
	if streamCount > 1 {
		for i := range m.mixedPCM {
			// 単純な正規化
			normalizedSample := float32(m.mixedPCM[i]) / float32(streamCount)
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

	// エンコーダーとデコーダーの参照を解放
	m.encoder = nil
	m.decoder = nil

	// ストリームとジッターバッファをクリア
	m.streams = make(map[uint32]*AudioStream)
	m.jitterBuffer = make(map[uint32][][]byte)
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
