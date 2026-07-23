package search

import (
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/registry"
)

// AnalyzerName is the Bleve analyzer name registered by this package's
// init(), backed directly by Tokenize (§11.6) rather than composed from
// Bleve's built-in tokenizer/token-filter chain, since Tokenize already
// produces the exact term set §11.6 wants (case variants, structural
// decomposition, no digit/letter splitting).
const AnalyzerName = "punakawan_technical"

type technicalAnalyzer struct{}

func (technicalAnalyzer) Analyze(input []byte) analysis.TokenStream {
	tokens := Tokenize(string(input))
	stream := make(analysis.TokenStream, 0, len(tokens))
	pos := 1
	offset := 0
	for _, tok := range tokens {
		start := offset
		end := start + len(tok)
		stream = append(stream, &analysis.Token{
			Term:     []byte(tok),
			Start:    start,
			End:      end,
			Position: pos,
			Type:     analysis.AlphaNumeric,
		})
		pos++
		offset = end + 1
	}
	return stream
}

func init() {
	err := registry.RegisterAnalyzer(AnalyzerName, func(_ map[string]interface{}, _ *registry.Cache) (analysis.Analyzer, error) {
		return technicalAnalyzer{}, nil
	})
	if err != nil {
		// Only fails on a duplicate name within the same process, which would
		// mean this package's own init ran twice - a programming error, not a
		// runtime condition callers can react to.
		panic(err)
	}
}
