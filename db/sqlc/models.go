// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0

package db

import (
	"github.com/jackc/pgx/v5/pgtype"
)

type GuildConfiguration struct {
	GuildID                     int64       `json:"guildId"`
	ModlogsChannel              pgtype.Int8 `json:"modlogsChannel"`
	LeaveJoinLogsChannel        pgtype.Int8 `json:"leaveJoinLogsChannel"`
	YoutubeNotificationsChannel pgtype.Int8 `json:"youtubeNotificationsChannel"`
	YoutubeNotificationsRole    pgtype.Int8 `json:"youtubeNotificationsRole"`
	RedditNotificationsChannel  pgtype.Int8 `json:"redditNotificationsChannel"`
	RedditNotificationsRole     pgtype.Int8 `json:"redditNotificationsRole"`
	StmpdNotificationsChannel   pgtype.Int8 `json:"stmpdNotificationsChannel"`
	StmpdNotificationsRole      pgtype.Int8 `json:"stmpdNotificationsRole"`
	WelcomesChannel             pgtype.Int8 `json:"welcomesChannel"`
	DeleteLogsChannel           pgtype.Int8 `json:"deleteLogsChannel"`
	EditLogsChannel             pgtype.Int8 `json:"editLogsChannel"`
	BotChannel                  pgtype.Int8 `json:"botChannel"`
	RadioVoiceChannel           pgtype.Int8 `json:"radioVoiceChannel"`
	NewsRole                    pgtype.Int8 `json:"newsRole"`
	XpMultiplier                float64     `json:"xpMultiplier"`
}

type JoinLeaveLog struct {
	MemberID int64            `json:"memberId"`
	Action   string           `json:"action"`
	Time     pgtype.Timestamp `json:"time"`
}

type Message struct {
	MessageID int64            `json:"messageId"`
	ChannelID int64            `json:"channelId"`
	AuthorID  int64            `json:"authorId"`
	Content   string           `json:"content"`
	Timestamp pgtype.Timestamp `json:"timestamp"`
	GuildID   int64            `json:"guildId"`
}

type Modlog struct {
	ID          int64            `json:"id"`
	UserID      int64            `json:"userId"`
	ModeratorID int64            `json:"moderatorId"`
	LogType     string           `json:"logType"`
	Reason      pgtype.Text      `json:"reason"`
	Time        pgtype.Timestamp `json:"time"`
}

type RedditPost struct {
	PostID string `json:"postId"`
}

type Song struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`
	Artists       string      `json:"artists"`
	ReleaseYear   int32       `json:"releaseYear"`
	ThumbnailUrl  pgtype.Text `json:"thumbnailUrl"`
	SpotifyUrl    pgtype.Text `json:"spotifyUrl"`
	AppleMusicUrl pgtype.Text `json:"appleMusicUrl"`
	YoutubeUrl    pgtype.Text `json:"youtubeUrl"`
	Lyrics        pgtype.Text `json:"lyrics"`
	IsUnreleased  bool        `json:"isUnreleased"`
	PureTitle     pgtype.Text `json:"pureTitle"`
}

type Tag struct {
	CreatorID int64            `json:"creatorId"`
	Content   string           `json:"content"`
	CreatedAt pgtype.Timestamp `json:"createdAt"`
	Uses      pgtype.Int4      `json:"uses"`
	Name      string           `json:"name"`
}

type User struct {
	ID           int64            `json:"id"`
	MessagesSent pgtype.Int4      `json:"messagesSent"`
	TotalXp      pgtype.Int4      `json:"totalXp"`
	LastXpAdded  pgtype.Timestamp `json:"lastXpAdded"`
	GarrixCoins  pgtype.Int8      `json:"garrixCoins"`
	InHand       pgtype.Int8      `json:"inHand"`
	GuildID      int64            `json:"guildId"`
}

type YoutubeVideo struct {
	VideoID string `json:"videoId"`
}
