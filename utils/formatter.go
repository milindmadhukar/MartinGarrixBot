package utils

import (
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

func CutString(str string, maxLen int) string {
	runes := []rune(str)
	if len(runes) > maxLen {
		return string(runes[0:maxLen-1]) + "…"
	}
	return string(runes)
}

func ExtractEmojiParts(emojiStr string) (name string, id snowflake.ID, animated bool) {
	trimmed := strings.Trim(emojiStr, "<>")

	parts := strings.Split(trimmed, ":")

	if len(parts) == 3 {
		if parts[0] == "a" {
			animated = true
		}

		name = parts[1]
		id = snowflake.MustParse(parts[2])
	}

	return name, id, animated
}
