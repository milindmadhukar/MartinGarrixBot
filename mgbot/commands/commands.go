package commands

import (
	"github.com/disgoorg/disgo/discord"
)

var Commands = []discord.ApplicationCommandCreate{
	ping,
	avatar,
	version,
}