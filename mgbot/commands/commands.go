package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

// TODO: Organize this also
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
	leaderboard,
	links,
	rank,
	//whois,
	version,
}

func SetupHandlers(b *mgbot.MartinGarrixBot) *handler.Mux {
	rootHandler := handler.New()

	// TODO: This is getting out of hand, find a better place to store and have something like cog baased loading with load unload commands?
	// TODO: Maybe add a help command

	rootHandler.Command("/balance", BalanceHandler(b))
	rootHandler.Command("/withdraw", WithdrawHandler(b))
	rootHandler.Command("/deposit", DepositHandler(b))
	rootHandler.Command("/give", GiveHandler(b))

	rootHandler.Command("/rank", RankHandler(b))
	rootHandler.Command("/leaderboard", LeaderboardHandler(b))

	rootHandler.Command("/links", LinksHandler(b))
	rootHandler.Autocomplete("/links", LinksAutocompleteHandler(b))

	rootHandler.Command("/version", VersionHandler(b))

	fun := handler.New()
	fun.Command("/8ball", EightBallHandler)
	fun.Command("/lyrics", LyricsHandler(b))
	fun.Autocomplete("/lyrics", LyricsAutocompleteHandler(b))
	fun.Command("/quiz", QuizHandler(b))
	rootHandler.Mount("/", fun)

	extras := handler.New()
	extras.Command("/avatar", AvatarHandler)
	extras.Command("/ping", PingHandler)
	rootHandler.Mount("/", extras)

	// h.Command("/whois", WhoisHandler)

	return rootHandler
}
