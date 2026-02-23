package utils

import "time"

type RedditResponse struct {
	Kind string `json:"kind,omitempty"`
	Data struct {
		Children []struct {
			Kind string `json:"kind,omitempty"`
			Data struct {
				Subreddit             string  `json:"subreddit,omitempty"`
				Selftext              string  `json:"selftext,omitempty"`
				AuthorFullname        string  `json:"author_fullname,omitempty"`
				Title                 string  `json:"title,omitempty"`
				SubredditNamePrefixed string  `json:"subreddit_name_prefixed,omitempty"`
				ThumbnailHeight       int     `json:"thumbnail_height,omitempty"`
				Name                  string  `json:"name,omitempty"`
				UpvoteRatio           float64 `json:"upvote_ratio,omitempty"`
				MediaEmbed            struct {
					Content   string `json:"content,omitempty"`
					Width     int    `json:"width,omitempty"`
					Scrolling bool   `json:"scrolling,omitempty"`
					Height    int    `json:"height,omitempty"`
				} `json:"media_embed,omitempty"`
				ThumbnailWidth        int    `json:"thumbnail_width,omitempty"`
				AuthorFlairTemplateID string `json:"author_flair_template_id,omitempty"`
				IsOriginalContent     bool   `json:"is_original_content,omitempty"`
				SecureMedia           struct {
					Type   string `json:"type,omitempty"`
					Oembed struct {
						ProviderURL     string `json:"provider_url,omitempty"`
						Version         string `json:"version,omitempty"`
						Title           string `json:"title,omitempty"`
						Type            string `json:"type,omitempty"`
						ThumbnailWidth  int    `json:"thumbnail_width,omitempty"`
						Height          int    `json:"height,omitempty"`
						Width           int    `json:"width,omitempty"`
						HTML            string `json:"html,omitempty"`
						AuthorName      string `json:"author_name,omitempty"`
						ProviderName    string `json:"provider_name,omitempty"`
						ThumbnailURL    string `json:"thumbnail_url,omitempty"`
						ThumbnailHeight int    `json:"thumbnail_height,omitempty"`
						AuthorURL       string `json:"author_url,omitempty"`
					} `json:"oembed,omitempty"`
				} `json:"secure_media,omitempty"`
				IsRedditMediaDomain bool `json:"is_reddit_media_domain,omitempty"`
				IsMeta              bool `json:"is_meta,omitempty"`
				Category            any  `json:"category,omitempty"`
				SecureMediaEmbed    struct {
					Content        string `json:"content,omitempty"`
					Width          int    `json:"width,omitempty"`
					Scrolling      bool   `json:"scrolling,omitempty"`
					MediaDomainURL string `json:"media_domain_url,omitempty"`
					Height         int    `json:"height,omitempty"`
				} `json:"secure_media_embed,omitempty"`
				LinkFlairText       string `json:"link_flair_text,omitempty"`
				Score               int    `json:"score,omitempty"`
				Thumbnail           string `json:"thumbnail,omitempty"`
				AuthorFlairRichtext []struct {
					E string `json:"e,omitempty"`
					T string `json:"t,omitempty"`
					A string `json:"a,omitempty"`
					U string `json:"u,omitempty"`
				} `json:"author_flair_richtext,omitempty"`
				SubredditType string  `json:"subreddit_type,omitempty"`
				Created       float64 `json:"created,omitempty"`
				SelftextHTML  any     `json:"selftext_html,omitempty"`
				Likes         any     `json:"likes,omitempty"`
				ViewCount     any     `json:"view_count,omitempty"`
				Spam          bool    `json:"spam,omitempty"`
				Preview       struct {
					Images []struct {
						Source struct {
							URL    string `json:"url,omitempty"`
							Width  int    `json:"width,omitempty"`
							Height int    `json:"height,omitempty"`
						} `json:"source,omitempty"`
						Resolutions []struct {
							URL    string `json:"url,omitempty"`
							Width  int    `json:"width,omitempty"`
							Height int    `json:"height,omitempty"`
						} `json:"resolutions,omitempty"`
						Variants struct {
						} `json:"variants,omitempty"`
						ID string `json:"id,omitempty"`
					} `json:"images,omitempty"`
					Enabled bool `json:"enabled,omitempty"`
				} `json:"preview,omitempty"`
				MediaOnly                bool    `json:"media_only,omitempty"`
				Spoiler                  bool    `json:"spoiler,omitempty"`
				AuthorFlairText          string  `json:"author_flair_text,omitempty"`
				Distinguished            any     `json:"distinguished,omitempty"`
				SubredditID              string  `json:"subreddit_id,omitempty"`
				LinkFlairBackgroundColor string  `json:"link_flair_background_color,omitempty"`
				ID                       string  `json:"id,omitempty"`
				Author                   string  `json:"author,omitempty"`
				NumComments              int     `json:"num_comments,omitempty"`
				Approved                 bool    `json:"approved,omitempty"`
				Permalink                string  `json:"permalink,omitempty"`
				Stickied                 bool    `json:"stickied,omitempty"`
				URL                      string  `json:"url,omitempty"`
				CreatedUtc               float64 `json:"created_utc,omitempty"`
				Media                    struct {
					Type   string `json:"type,omitempty"`
					Oembed struct {
						ProviderURL     string `json:"provider_url,omitempty"`
						Version         string `json:"version,omitempty"`
						Title           string `json:"title,omitempty"`
						Type            string `json:"type,omitempty"`
						ThumbnailWidth  int    `json:"thumbnail_width,omitempty"`
						Height          int    `json:"height,omitempty"`
						Width           int    `json:"width,omitempty"`
						HTML            string `json:"html,omitempty"`
						AuthorName      string `json:"author_name,omitempty"`
						ProviderName    string `json:"provider_name,omitempty"`
						ThumbnailURL    string `json:"thumbnail_url,omitempty"`
						ThumbnailHeight int    `json:"thumbnail_height,omitempty"`
						AuthorURL       string `json:"author_url,omitempty"`
					} `json:"oembed,omitempty"`
				} `json:"media,omitempty"`
				IsVideo bool `json:"is_video,omitempty"`
			} `json:"data,omitempty"`
		} `json:"children,omitempty"`
		Before any `json:"before,omitempty"`
	} `json:"data,omitempty"`
}

type RedditToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int       `json:"expires_in"`
	Scope       string    `json:"scope"`
	ExpiresAt   time.Time `json:"-"`
}

type VideoData struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailUrl string `json:"thumbnail_url"`
	Thumbnail    []byte `json:"-"`
}

type StmpdRelease struct {
	Name          string `json:"name"`
	Artists       string `json:"artists"`
	ReleaseYear   int    `json:"year"`
	Thumbnail     string `json:"thumbnail"`
	SpotifyURL    string `json:"spotify_url,omitempty"`
	AppleMusicUrl string `json:"apple_url,omitempty"`
	YoutubeURL    string `json:"youtube_url,omitempty"`
}

type UniqueSong struct {
	Name        string `json:"name"`
	Artists     string `json:"artists"`
	ReleaseDate string `json:"release_date"`
}

type TourShow struct {
	ShowName  string    `json:"show_name"`
	City      string    `json:"city"`
	Country   string    `json:"country"`
	Venue     string    `json:"venue"`
	ShowDate  time.Time `json:"show_date"`
	TicketURL string    `json:"ticket_url,omitempty"`
}

// UserLevelData represents the user's level data.
type UserLevelData struct {
	Lvl          int
	XpForNextLvl int32
	CurrentXp    int32
}
