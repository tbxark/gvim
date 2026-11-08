package fuzzy

// Pattern is a fuzzy query prepared once and matched against many targets. It
// gives the same result as Score for the same query and target, so the file
// finder and the command palette agree on what a query matches.
type Pattern struct {
	terms []queryTerm
}

// NewPattern splits query on whitespace and lower-cases each term. A blank
// query yields a pattern that matches everything with score 0.
func NewPattern(query string) Pattern {
	return Pattern{terms: prepareQuery(query)}
}

// Match scores target against the pattern. It is Score for a prepared query.
func (p Pattern) Match(target string) (score int, indexes []int, ok bool) {
	r := matchTerms(p.terms, target)
	return r.score, r.positions, r.matched
}
