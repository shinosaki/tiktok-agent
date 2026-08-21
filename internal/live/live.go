package live

import (
	"encoding/json"
	"fmt"
)

// Live は検出されたライブ配信を表す。テンプレートに渡すデータモデル。
type Live struct {
	RoomID      string
	Username    string
	Nickname    string
	Title       string
	StreamURL   string
	ViewerCount int
	LikeCount   int64
}

// feedResponse は webcast feed API レスポンスの固定スキーマ。
type feedResponse struct {
	StatusCode int        `json:"status_code"`
	Data       []feedItem `json:"data"`
}

type feedItem struct {
	Type int  `json:"type"`
	Data room `json:"data"`
}

type room struct {
	IDStr     string    `json:"id_str"`
	Title     string    `json:"title"`
	Status    int       `json:"status"`
	UserCount int       `json:"user_count"`
	LikeCount int64     `json:"like_count"`
	Owner     owner     `json:"owner"`
	StreamURL streamURL `json:"stream_url"`
}

type owner struct {
	Nickname  string `json:"nickname"`
	DisplayID string `json:"display_id"`
}

type streamURL struct {
	RTMPPullURL string            `json:"rtmp_pull_url"`
	FLVPullURL  map[string]string `json:"flv_pull_url"`
}

// ParseFeed は cURL コマンドの stdout を JSON としてパースし、ライブ一覧を返す。
func ParseFeed(data []byte) ([]Live, error) {
	var resp feedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("invalid feed JSON: %w", err)
	}
	if resp.StatusCode != 0 {
		return nil, fmt.Errorf("feed API returned status_code %d", resp.StatusCode)
	}
	lives := make([]Live, 0, len(resp.Data))
	for _, item := range resp.Data {
		if item.Data.IDStr == "" {
			continue
		}
		lives = append(lives, item.Data.toLive())
	}
	return lives, nil
}

func (r room) toLive() Live {
	stream := r.StreamURL.RTMPPullURL
	if stream == "" {
		stream = r.StreamURL.FLVPullURL["HD1"]
	}
	return Live{
		RoomID:      r.IDStr,
		Username:    r.Owner.DisplayID,
		Nickname:    r.Owner.Nickname,
		Title:       r.Title,
		StreamURL:   stream,
		ViewerCount: r.UserCount,
		LikeCount:   r.LikeCount,
	}
}
