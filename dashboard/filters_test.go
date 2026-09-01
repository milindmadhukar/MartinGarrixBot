package dashboard

import (
	"net/url"
	"testing"
	"time"
)

// TestOptTimestampConvertsFromLocalDate is the date-filter half of the timezone
// trap. The user types a wall-clock date in their own zone; the column holds a
// naive UTC instant. Parsing the date as UTC would shift every filter by the
// zone offset -- five and a half hours here, enough to silently drop rows.
func TestOptTimestampConvertsFromLocalDate(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("loading location: %v", err)
	}

	// Midnight on 2026-01-02 IST is 18:30 on 2026-01-01 UTC.
	got := optTimestamp("2026-01-02", ist, false)
	if !got.Valid {
		t.Fatal("a valid date should produce a bound parameter")
	}
	want := time.Date(2026, 1, 1, 18, 30, 0, 0, time.UTC)
	if !got.Time.Equal(want) {
		t.Errorf("after = %v, want %v", got.Time, want)
	}

	// `before` is exclusive of the following midnight, so filtering "to the
	// 2nd" includes the whole of the 2nd as a user expects.
	end := optTimestamp("2026-01-02", ist, true)
	wantEnd := time.Date(2026, 1, 2, 18, 30, 0, 0, time.UTC)
	if !end.Time.Equal(wantEnd) {
		t.Errorf("before = %v, want %v", end.Time, wantEnd)
	}
	if !end.Time.After(got.Time) {
		t.Error("the end bound must be after the start bound for the same date")
	}
}

func TestOptTimestampIgnoresJunk(t *testing.T) {
	for _, value := range []string{"", "not-a-date", "2026-13-45", "01/02/2026"} {
		if got := optTimestamp(value, time.UTC, false); got.Valid {
			t.Errorf("optTimestamp(%q) should be unset, got %v", value, got.Time)
		}
	}
}

func TestOptFiltersUnsetOnEmpty(t *testing.T) {
	if optText("").Valid {
		t.Error("an empty text filter should be unset, not an empty-string match")
	}
	if optInt8("").Valid || optInt8("not-a-number").Valid {
		t.Error("a blank or malformed ID filter should be unset")
	}
	// A malformed ID must not silently become 0 -- that would filter to user 0
	// and show an empty table with no explanation.
	if got := optInt8("abc"); got.Valid {
		t.Errorf("malformed ID parsed to %d", got.Int64)
	}
	if got := optInt8("12345"); !got.Valid || got.Int64 != 12345 {
		t.Errorf("optInt8(\"12345\") = %+v", got)
	}

	if optBool("").Valid || optBool("maybe").Valid {
		t.Error("an unrecognised boolean filter should be unset")
	}
	if got := optBool("false"); !got.Valid || got.Bool {
		t.Errorf(`optBool("false") = %+v, want a set false`, got)
	}
}

func TestPagination(t *testing.T) {
	t.Run("counts pages", func(t *testing.T) {
		p := newPagination(1, 50, 120, "")
		if p.TotalPages != 3 {
			t.Errorf("TotalPages = %d, want 3", p.TotalPages)
		}
		if p.HasPrev || !p.HasNext {
			t.Errorf("page 1 of 3: HasPrev=%v HasNext=%v", p.HasPrev, p.HasNext)
		}
	})

	t.Run("exact multiple does not add a blank page", func(t *testing.T) {
		if got := newPagination(1, 50, 100, "").TotalPages; got != 2 {
			t.Errorf("TotalPages = %d, want 2", got)
		}
	})

	t.Run("empty result still has one page", func(t *testing.T) {
		p := newPagination(1, 50, 0, "")
		if p.TotalPages != 1 {
			t.Errorf("TotalPages = %d, want 1", p.TotalPages)
		}
		if p.HasNext {
			t.Error("an empty result should not offer a next page")
		}
	})

	t.Run("last page has no next", func(t *testing.T) {
		p := newPagination(3, 50, 120, "")
		if p.HasNext {
			t.Error("the last page should not offer a next page")
		}
		if !p.HasPrev {
			t.Error("the last page should offer a previous page")
		}
	})
}

func TestParsePagingClamps(t *testing.T) {
	cases := []struct {
		query        string
		wantPage     int
		wantPageSize int
	}{
		{"", 1, defaultPageSize},
		{"page=3", 3, defaultPageSize},
		{"page=0", 1, defaultPageSize},
		{"page=-5", 1, defaultPageSize},
		{"page=abc", 1, defaultPageSize},
		{"size=10", 1, 10},
		// An unbounded page size is a way to ask for the whole table.
		{"size=99999", 1, maxPageSize},
		{"size=0", 1, defaultPageSize},
	}

	for _, tc := range cases {
		values, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatalf("ParseQuery(%q): %v", tc.query, err)
		}
		page, size := parsePaging(values)
		if page != tc.wantPage || size != tc.wantPageSize {
			t.Errorf("parsePaging(%q) = (%d, %d), want (%d, %d)",
				tc.query, page, size, tc.wantPage, tc.wantPageSize)
		}
	}
}

// TestFilterQueryDropsPage keeps page links from accumulating stale page
// numbers while preserving the filters the user chose.
func TestFilterQueryDropsPage(t *testing.T) {
	values, _ := url.ParseQuery("page=4&type=ban&user=123&before=")
	got := filterQuery(values)

	if contains(got, "page=") {
		t.Errorf("filterQuery kept the page number: %q", got)
	}
	if !contains(got, "type=ban") || !contains(got, "user=123") {
		t.Errorf("filterQuery dropped a live filter: %q", got)
	}
	if contains(got, "before=") {
		t.Errorf("filterQuery kept an empty filter: %q", got)
	}

	if got := filterQuery(url.Values{}); got != "" {
		t.Errorf("filterQuery with no filters = %q, want empty", got)
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 {
		t.Fatalf("dedupe returned %v, want 3 entries", got)
	}
	// Order matters: the batch resolve response is matched back by ID, and a
	// stable order keeps the request reproducible.
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("dedupe did not preserve first-seen order: %v", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
