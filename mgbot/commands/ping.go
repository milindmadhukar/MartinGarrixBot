package commands

import (
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/rest"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var ping = discord.SlashCommandCreate{
	Name:        "ping",
	Description: "Check the latency of the bot from the server.",
}

func PingHandler(e *handler.CommandEvent) error {
	var gatewayPing string
	if e.Client().HasGateway() {
		gatewayPing = e.Client().Gateway().Latency().String()
	}

	eb := discord.NewEmbedBuilder().
		SetTitle("Pong! \U0001F3D3").
		AddField("Rest", "loading...", false).
		AddField("Gateway", gatewayPing, false).
		SetColor(utils.ColorSuccess)

	defer func() {
		var start int64
		_, _ = e.Client().Rest().GetBotApplicationInfo(func(config *rest.RequestConfig) {
			start = time.Now().UnixNano()
		})
		duration := time.Now().UnixNano() - start
		eb.SetField(0, "Rest", time.Duration(duration).String(), false)
		if _, err := e.Client().Rest().UpdateInteractionResponse(e.ApplicationID(), e.Token(), discord.MessageUpdate{Embeds: &[]discord.Embed{eb.Build()}}); err != nil {
			slog.Error("Failed update interaction", slog.Any("err", err))
		}
	}()

	return e.Respond(
		discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
			SetEmbeds(eb.Build()).
			SetEphemeral(true).
			Build(),
	)
}