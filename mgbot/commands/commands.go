package commands

import (
	"github.com/disgoorg/disgo/discord"
)

var Commands = []discord.ApplicationCommandCreate{
	ping,
	avatar,
	eightball,
	lyrics,
	quiz,
	balance,
	//whois,
	version,
}