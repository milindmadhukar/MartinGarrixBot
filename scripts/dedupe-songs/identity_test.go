package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// row builds a catalogue row for the pair rules to judge. Only the columns those rules
// read are settable; everything else on the real row is provenance the merge uses and
// the decision does not.
type row struct {
	id       int64
	name     string
	artists  string
	mix      string
	youtube  string
	apple    string
	spotify  string
	slug     string
	parentOf int64
}

func (r row) build() db.GetSongsForSubsetDedupeRow {
	out := db.GetSongsForSubsetDedupeRow{
		ID:            r.id,
		Name:          r.name,
		Artists:       r.artists,
		MixName:       utils.Text(r.mix),
		YoutubeUrl:    utils.Text(r.youtube),
		AppleMusicUrl: utils.Text(r.apple),
		SpotifyUrl:    utils.Text(r.spotify),
		StmpdSlug:     utils.Text(r.slug),
	}
	if r.parentOf != 0 {
		out.ParentSongID = pgtype.Int8{Int64: r.parentOf, Valid: true}
	}
	return out
}

// The pair this pass was written for. Neither artist set contains the other -- IBRA is
// what Ibranovski is called now -- so nothing that reads the credits can pair them.
var (
	monsterBeatport = row{
		id: 331, name: "Monster", artists: "Sikdope, Ibranovski", mix: "Extended MIx",
		youtube: "https://www.youtube.com/watch?v=NxhlqzCtc2w",
		apple:   "https://music.apple.com/de/album/monster/1526794459?i=1526794460&l=en",
		spotify: "https://open.spotify.com/track/1g2jgVFVPmBM32boNyFfBk",
	}
	monsterCatalogue = row{
		id: 15649, name: "Monster", artists: "Sikdope & IBRA",
		youtube: "https://www.youtube.com/watch?v=NxhlqzCtc2w",
		apple:   "https://music.apple.com/nl/album/monster-single/1526794459",
		spotify: "https://open.spotify.com/album/4jzSiR2JLIoZiEWO6SZ2u3",
		slug:    "sikdope-ibra-monster-2020-9-3",
	}
)

func TestIdentityTokensPairARenamedAct(t *testing.T) {
	a := shared(identityTokens(monsterBeatport.build()), identityTokens(monsterCatalogue.build()))
	if len(a) == 0 {
		t.Fatal("the two Monster rows share no identifier; nothing would pair them")
	}

	// Both services must contribute. The Apple one is the reason the pass reads the
	// release id rather than the track id: only one of these rows names a track.
	want := map[string]bool{
		"youtube https://www.youtube.com/watch?v=NxhlqzCtc2w": false,
		"apple 1526794459": false,
	}
	for _, token := range a {
		if _, ok := want[token]; ok {
			want[token] = true
		}
	}
	for token, found := range want {
		if !found {
			t.Errorf("expected the rows to share %q", token)
		}
	}

	// Spotify must NOT pair them: one links a track and the other an album, and a bare
	// id compared across those two spaces is a false match waiting to happen.
	for _, token := range a {
		if token == "spotify track:1g2jgVFVPmBM32boNyFfBk" || token == "spotify album:4jzSiR2JLIoZiEWO6SZ2u3" {
			t.Errorf("a track id and an album id must not compare equal: %q", token)
		}
	}
}

func TestWhyNotOneRecording(t *testing.T) {
	tests := []struct {
		name string
		a, b row
		want string
	}{
		{
			name: "a renamed act is still one recording",
			a:    monsterBeatport, b: monsterCatalogue,
			want: "",
		},
		{
			// The commonest shape: an EP and a track on it share the release's video.
			// Both are Goja, both link to HCFM7DlzJls, and both belong in the table.
			name: "a release and a track on it are different songs",
			a:    row{id: 56, name: "Design EP", artists: "Goja", youtube: "https://www.youtube.com/watch?v=HCFM7DlzJls"},
			b:    row{id: 15276, name: "Do You Know", artists: "Goja", youtube: "https://www.youtube.com/watch?v=HCFM7DlzJls"},
			want: "different songs",
		},
		{
			name: "a remix published against the original's release is not the original",
			a:    row{id: 1, name: "Catharina", artists: "Martin Garrix", apple: "https://music.apple.com/nl/album/catharina/123456789"},
			b:    row{id: 2, name: "Catharina", artists: "Martin Garrix, Surf Mesa", mix: "Surf Mesa Remix", apple: "https://music.apple.com/nl/album/catharina/123456789"},
			want: "different renditions: (none) and surfmesaremix",
		},
		{
			// An extended mix and an unnamed version are the same recording, which is
			// the whole point of RenditionsAgree and must survive into this pass.
			name: "a default rendition agrees with none",
			a:    row{id: 1, name: "All We Got", artists: "Shy Baboon & Maejor", mix: "Extended Mix", youtube: "https://www.youtube.com/watch?v=tAZV1RBSJe4"},
			b:    row{id: 2, name: "All We Got", artists: "Maejor, Shy Baboon", youtube: "https://www.youtube.com/watch?v=tAZV1RBSJe4"},
			want: "",
		},
		{
			name: "two catalogue slugs are two releases",
			a:    row{id: 1, name: "Void", artists: "Seth Hills", slug: "seth-hills-void", youtube: "https://www.youtube.com/watch?v=abc"},
			b:    row{id: 2, name: "Void", artists: "Seth Hills", slug: "seth-hills-void-ep", youtube: "https://www.youtube.com/watch?v=abc"},
			want: "separate releases in the STMPD catalogue",
		},
		{
			// One row missing a slug is not a disagreement, and this is the shape the
			// Monster pair has -- the beatport row has no slug at all.
			name: "one slug and one absent slug is not a disagreement",
			a:    row{id: 1, name: "Void", artists: "Seth Hills", youtube: "https://www.youtube.com/watch?v=abc"},
			b:    row{id: 2, name: "Void", artists: "Seth Hills", slug: "seth-hills-void", youtube: "https://www.youtube.com/watch?v=abc"},
			want: "",
		},
		{
			name: "a row already filed as a rendition of the other is left to link-remix-parents",
			a:    row{id: 1, name: "Hero", artists: "Martin Garrix", youtube: "https://www.youtube.com/watch?v=abc"},
			b:    row{id: 2, name: "Hero", artists: "Martin Garrix", parentOf: 1, youtube: "https://www.youtube.com/watch?v=abc"},
			want: "one row is already filed as a rendition of the other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := tt.a.build(), tt.b.build()
			if got := whyNotOneRecording(a, b); got != tt.want {
				t.Errorf("whyNotOneRecording(%d, %d) = %q; want %q", a.ID, b.ID, got, tt.want)
			}
			// The verdict cannot depend on which row was read first.
			if got := whyNotOneRecording(b, a); got != tt.want {
				t.Errorf("reversed: whyNotOneRecording(%d, %d) = %q; want %q", b.ID, a.ID, got, tt.want)
			}
		})
	}
}

// shared returns the tokens present in both lists.
func shared(a, b []string) []string {
	in := make(map[string]bool, len(a))
	for _, x := range a {
		in[x] = true
	}
	var out []string
	for _, y := range b {
		if in[y] {
			out = append(out, y)
		}
	}
	return out
}
