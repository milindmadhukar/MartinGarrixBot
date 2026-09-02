package utils_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// newTestRadioManager builds a manager the way production does. Always go
// through the constructor rather than a struct literal: it allocates the maps
// that SetActive, SetPaused and SetCurrentTrack write into, and a literal would
// panic on a nil map. disgolink.New allocates a client but opens no connection,
// so nothing here touches the network.
func newTestRadioManager(t *testing.T) *utils.RadioManager {
	t.Helper()
	return utils.NewRadioManager(snowflake.ID(1))
}

func TestRadioManager_SkipVoteQuorum(t *testing.T) {
	t.Parallel()

	// votesNeeded is reported to the user; shouldSkip actually triggers the
	// skip. They must agree, otherwise the bot would either skip early or tell
	// people a vote is needed that never lands.
	for totalMembers := 0; totalMembers <= 50; totalMembers++ {
		rm := newTestRadioManager(t)
		guild := snowflake.ID(1000 + totalMembers)

		var (
			reportedNeeded int
			skippedAt      = -1
		)

		// Vote one distinct member at a time.
		for voter := 1; voter <= max(totalMembers, 1); voter++ {
			needed, current, shouldSkip := rm.AddSkipVote(guild, snowflake.ID(voter), totalMembers)
			reportedNeeded = needed

			if current != voter {
				t.Fatalf("totalMembers=%d: after %d voters, currentVotes = %d",
					totalMembers, voter, current)
			}
			if shouldSkip && skippedAt == -1 {
				skippedAt = voter
			}
		}

		wantNeeded := (totalMembers / 2) + 1
		if reportedNeeded != wantNeeded {
			t.Errorf("totalMembers=%d: votesNeeded = %d, want %d",
				totalMembers, reportedNeeded, wantNeeded)
		}
		if skippedAt != wantNeeded {
			t.Errorf("totalMembers=%d: skip fired at vote %d, but %d votes were reported as needed",
				totalMembers, skippedAt, wantNeeded)
		}
	}
}

func TestRadioManager_SkipVotesAreDeduplicatedPerUser(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)
	guild, user := snowflake.ID(1), snowflake.ID(42)

	for range 5 {
		_, current, _ := rm.AddSkipVote(guild, user, 10)
		if current != 1 {
			t.Fatalf("currentVotes = %d after repeated votes from one user, want 1", current)
		}
	}
}

func TestRadioManager_GetSkipVoteStatus(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)
	guild := snowflake.ID(1)
	voter, bystander := snowflake.ID(10), snowflake.ID(11)

	t.Run("before any vote", func(t *testing.T) {
		needed, current, hasVoted := rm.GetSkipVoteStatus(guild, voter, 4)
		if needed != 3 || current != 0 || hasVoted {
			t.Errorf("got (needed=%d, current=%d, hasVoted=%v), want (3, 0, false)",
				needed, current, hasVoted)
		}
	})

	rm.AddSkipVote(guild, voter, 4)

	t.Run("the voter is recognised", func(t *testing.T) {
		_, current, hasVoted := rm.GetSkipVoteStatus(guild, voter, 4)
		if current != 1 || !hasVoted {
			t.Errorf("got (current=%d, hasVoted=%v), want (1, true)", current, hasVoted)
		}
	})

	t.Run("someone who has not voted is not", func(t *testing.T) {
		_, _, hasVoted := rm.GetSkipVoteStatus(guild, bystander, 4)
		if hasVoted {
			t.Error("a member who has not voted was reported as having voted")
		}
	})
}

func TestRadioManager_ResetSkipVotes(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)
	guild := snowflake.ID(1)

	rm.AddSkipVote(guild, snowflake.ID(10), 4)
	rm.AddSkipVote(guild, snowflake.ID(11), 4)

	rm.ResetSkipVotes(guild)

	if _, current, hasVoted := rm.GetSkipVoteStatus(guild, snowflake.ID(10), 4); current != 0 || hasVoted {
		t.Errorf("after reset got (current=%d, hasVoted=%v), want (0, false)", current, hasVoted)
	}
}

// Votes are only cleared when a new track starts, and nothing checks that a
// voter is still in the channel. If members leave, the recorded votes can
// outnumber the channel. Pinned so the behaviour is visible.
func TestRadioManager_VotesCanExceedMemberCount(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)
	guild := snowflake.ID(1)

	for voter := 1; voter <= 5; voter++ {
		rm.AddSkipVote(guild, snowflake.ID(voter), 5)
	}

	// Everyone but one member leaves the channel.
	_, current, shouldSkip := rm.AddSkipVote(guild, snowflake.ID(6), 1)
	if current != 6 {
		t.Errorf("currentVotes = %d, want 6 (stale votes are not pruned)", current)
	}
	if !shouldSkip {
		t.Error("expected the stale votes to still trigger a skip")
	}
}

func TestRadioManager_GuildStateIsIndependent(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)
	first, second := snowflake.ID(1), snowflake.ID(2)

	t.Run("active state", func(t *testing.T) {
		if rm.IsActive(first) {
			t.Error("a guild should not start active")
		}

		rm.SetActive(first, true)
		if !rm.IsActive(first) {
			t.Error("expected the guild to be active")
		}
		if rm.IsActive(second) {
			t.Error("setting one guild active affected another")
		}

		rm.SetActive(first, false)
		if rm.IsActive(first) {
			t.Error("expected the guild to be inactive again")
		}
	})

	t.Run("paused state", func(t *testing.T) {
		if rm.IsPaused(first) {
			t.Error("a guild should not start paused")
		}

		rm.SetPaused(first, true)
		if !rm.IsPaused(first) || rm.IsPaused(second) {
			t.Error("paused state leaked between guilds")
		}
	})

	t.Run("current track", func(t *testing.T) {
		if _, ok := rm.GetCurrentTrack(first); ok {
			t.Error("a guild should not start with a track")
		}

		rm.SetCurrentTrack(first, 99, "Martin Garrix", "Animals")

		track, ok := rm.GetCurrentTrack(first)
		if !ok {
			t.Fatal("expected a current track")
		}
		if track.SongID != 99 || track.Artist != "Martin Garrix" || track.SongName != "Animals" {
			t.Errorf("track = %+v, want the values that were set", track)
		}

		if _, ok := rm.GetCurrentTrack(second); ok {
			t.Error("the track leaked into another guild")
		}
	})
}

func TestRadioManager_GetActiveGuilds(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)

	if got := rm.GetActiveGuilds(); len(got) != 0 {
		t.Errorf("got %d active guilds, want none", len(got))
	}

	rm.SetActive(snowflake.ID(1), true)
	rm.SetActive(snowflake.ID(2), false)
	rm.SetActive(snowflake.ID(3), true)

	got := rm.GetActiveGuilds()
	slices.Sort(got) // map iteration order is not defined

	want := []snowflake.ID{1, 3}
	if !slices.Equal(got, want) {
		t.Errorf("GetActiveGuilds() = %v, want %v", got, want)
	}
}

// RadioManager is the only shared mutable state in the bot, written from
// gateway event listeners and command handlers at the same time. This is what
// makes -race worth running.
func TestRadioManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	rm := newTestRadioManager(t)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			guild := snowflake.ID(i%5 + 1)
			user := snowflake.ID(i)

			rm.SetActive(guild, true)
			rm.IsActive(guild)
			rm.SetPaused(guild, i%2 == 0)
			rm.IsPaused(guild)
			rm.SetCurrentTrack(guild, int64(i), "artist", "song")
			rm.GetCurrentTrack(guild)
			rm.AddSkipVote(guild, user, 10)
			rm.GetSkipVoteStatus(guild, user, 10)
			rm.GetActiveGuilds()
			if i%10 == 0 {
				rm.ResetSkipVotes(guild)
			}
		}(i)
	}
	wg.Wait()
}
