//go:build ruleguard

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

const (
	ifCond     = `if $cond { $*_ }`
	ifInitCond = `if $init; $cond { $*_ }`
	elseIf     = ` else if $*_ { $*_ }`
	elseBlock  = ` else { $*_ }`

	multiLineHeader = `if statement header must fit on a single line`
)

func singleLineIf(m dsl.Matcher) {
	m.Match(
		ifCond,
		ifCond+elseBlock,
		ifCond+elseIf,
		ifCond+elseIf+elseBlock,
		ifCond+elseIf+elseIf,
		ifCond+elseIf+elseIf+elseBlock,
		ifCond+elseIf+elseIf+elseIf,
		ifCond+elseIf+elseIf+elseIf+elseBlock,
	).
		Where(m["cond"].Text.Matches(`\n`)).
		Report(multiLineHeader)

	m.Match(
		ifInitCond,
		ifInitCond+elseBlock,
		ifInitCond+elseIf,
		ifInitCond+elseIf+elseBlock,
		ifInitCond+elseIf+elseIf,
		ifInitCond+elseIf+elseIf+elseBlock,
		ifInitCond+elseIf+elseIf+elseIf,
		ifInitCond+elseIf+elseIf+elseIf+elseBlock,
	).
		Where(
			m["init"].Text.Matches(`\n`) ||
				m["cond"].Text.Matches(`\n`) ||
				m["init"].Line != m["cond"].Line,
		).
		Report(multiLineHeader)
}
