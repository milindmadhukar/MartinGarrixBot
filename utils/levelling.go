package utils

func FXpForNextLevel(lvl int) int {
	return 5*lvl*lvl + 50*lvl + 100
}

// GetTotalXp returns the total XP required based on the given level.
func GetTotalXp(lvl int) int {
	totalSum := 0
	for i := 0; i < lvl; i++ {
		totalSum += FXpForNextLevel(i)
	}
	return totalSum
}

// GetUserLevel calculates the user's level based on total XP.
func GetUserLevel(totalXp int) int {
	lvl := 0
	totalSum := 0
	for totalSum <= totalXp {
		totalSum += FXpForNextLevel(lvl)
		lvl++
	}
	return lvl - 1
}

// GetUserLevelData returns the user's level data based on total XP.
func GetUserLevelData(totalXp int) UserLevelData {
	lvl := GetUserLevel(totalXp)
	return UserLevelData{
		Lvl:          lvl,
		XpForNextLvl: FXpForNextLevel(lvl),
		CurrentXp:    totalXp - GetTotalXp(lvl),
	}
}