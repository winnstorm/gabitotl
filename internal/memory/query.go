package memory

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

// Retrieval caps: memory is consulted by relevance, never loaded in full.
const (
	DefaultPerQuery = 8
	DefaultTotal    = 20
	maxTerms        = 6
	minTermLen      = 3
)

// QueryOptions tunes BuildQueries and Retrieve; zero values use the defaults.
type QueryOptions struct {
	PerQuery int
	Total    int
}

func (o QueryOptions) perQuery() int {
	if o.PerQuery > 0 {
		return o.PerQuery
	}
	return DefaultPerQuery
}

func (o QueryOptions) total() int {
	if o.Total > 0 {
		return o.Total
	}
	return DefaultTotal
}

var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		the and for with that this from into over about are was were been being have has had not but you your
		our their its can could should would will shall may might must let lets please want need make use using
		sigamos seguir con sin los las una uno unos unas del por para que como pero este esta esto estos estas
		ese esa eso son fue ser sea hay tiene tener hacer hace quiero necesito favor mas muy ahora todo todos
		toda todas nos les sus mis tus the then than also just any all how what when where which who why`) {
		m[w] = true
	}
	return m
}()

// ExtractTerms derives a small ordered set of search terms from a free-text
// request. Stopwords (EN/ES) and very short words are dropped; identifiers
// (snake_case, kebab-case, dotted names, camelCase) are kept whole.
func ExtractTerms(request string) []string {
	isSep := func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/')
	}
	var terms []string
	for _, raw := range strings.FieldsFunc(request, isSep) {
		t := strings.Trim(raw, "-_./")
		if len([]rune(t)) < minTermLen {
			continue
		}
		low := strings.ToLower(t)
		if stopwords[low] || slices.Contains(terms, low) {
			continue
		}
		terms = append(terms, low)
		if len(terms) == maxTerms {
			break
		}
	}
	return terms
}

// standing are the queries issued for every relevant piece of work.
var standing = []struct {
	text string
	typ  EntryType
}{
	{"project map", TypeProjectMap},
	{"progress", TypeProgress},
	{"current task pending", TypePending},
	{"convention", TypeConvention},
	{"known errors bugfix", TypeBugfix},
}

// BuildQueries returns the request-derived query (when the request yields any
// term) followed by the standing queries: project map, recent progress, current
// task, conventions and known errors. Every query is project scoped and capped.
func BuildQueries(request, project string, opt QueryOptions) []Query {
	limit := opt.perQuery()
	var qs []Query
	if terms := ExtractTerms(request); len(terms) > 0 {
		qs = append(qs, Query{Text: strings.Join(terms, " "), Project: project, Limit: limit, MatchAny: true})
	}
	for _, s := range standing {
		qs = append(qs, Query{Text: s.text, Type: s.typ, Project: project, Limit: limit, MatchAny: true})
	}
	return qs
}

// Retrieve runs the queries in order against p, de-duplicates entries and stops
// at the total cap. A failing query is skipped; the first error is returned only
// when nothing could be retrieved at all.
func Retrieve(ctx context.Context, p Provider, qs []Query, opt QueryOptions) ([]Entry, error) {
	total := opt.total()
	var out []Entry
	seen := map[string]bool{}
	var firstErr error
	ok := 0
	for _, q := range qs {
		if len(out) >= total {
			break
		}
		if q.Limit <= 0 || q.Limit > opt.perQuery() {
			q.Limit = opt.perQuery()
		}
		got, err := p.Search(ctx, q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ok++
		for _, e := range got {
			k := string(e.ID)
			if k == "" {
				k = string(e.Type) + "\x00" + e.Project + "\x00" + e.Title
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, e)
			if len(out) >= total {
				break
			}
		}
	}
	if ok == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
