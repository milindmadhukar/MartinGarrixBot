package utils

import (
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

func ReplyToMessage(client bot.Client, channelID snowflake.ID, message discord.Message, replyMessage string) *discord.Message {
	messageBuild := discord.NewMessageCreateBuilder().
		SetContent(replyMessage).
		SetMessageReferenceByID(message.ID).
		SetAllowedMentions(&discord.AllowedMentions{
			RepliedUser: false,
		}).
		Build()

	replyMesssage, _ := client.Rest().CreateMessage(channelID, messageBuild)

	return replyMesssage
}

func ReplyToMessageDeleteAfter(client bot.Client, channelID snowflake.ID, message discord.Message, replyMessage string, deleteAfter int) *discord.Message {
	replyMesssage := ReplyToMessage(client, channelID, message, replyMessage)
	client.Rest().DeleteMessage(channelID, replyMesssage.ID, rest.WithDelay(time.Second*time.Duration(deleteAfter)))
	return replyMesssage
}
