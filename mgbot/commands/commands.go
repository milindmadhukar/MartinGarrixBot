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
	withdraw,
	deposit,
	give,
	//whois,
	version,
}