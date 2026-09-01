package utils

import "github.com/disgoorg/disgo/discord"

// discord.Embed's With* methods take a value receiver and return a copy, so
// their result always has to be assigned back.

func SuccessEmbed(title, description string) discord.Embed {
	eb := discord.NewEmbed().
		WithTitle(CutString(TickEmoji+" "+title, 256)).
		WithColor(ColorSuccess)

	if description != "" {
		eb = eb.WithDescription(CutString(description, 2048))
	}

	return eb
}

func FailureEmbed(title, description string) discord.Embed {
	eb := discord.NewEmbed().
		WithTitle(CutString(CrossEmoji+" "+title, 256)).
		WithColor(ColorError)

	if description != "" {
		eb = eb.WithDescription(CutString(description, 2048))
	}

	return eb
}
