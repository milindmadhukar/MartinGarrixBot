package utils

import "github.com/disgoorg/disgo/discord"

func SuccessEmbed(title, description string) discord.Embed {
	eb := discord.NewEmbedBuilder().
		SetTitle(CutString("<a:tick:810462879374770186> "+title, 256))

	if description != "" {
		eb.Description = (CutString(description, 2048))
	}

	eb.Color = ColorSuccess

	return eb.Build()
}

func FailureEmbed(title, description string) discord.Embed {
	eb := discord.NewEmbedBuilder().
		SetTitle(CutString("<a:cross:810462920810561556> "+title, 256))

	if description != "" {
		eb.Description = (CutString(description, 2048))
	}

	eb.Color = ColorError

	return eb.Build()
}