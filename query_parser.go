package main

import (
	"strings"

	bleve "github.com/blevesearch/bleve/v2"
	bleveQuery "github.com/blevesearch/bleve/v2/search/query"
)

type TokenType int

const (
	TokenWord TokenType = iota
	TokenOR
	TokenAND
	TokenNOT
	TokenLParen
	TokenRParen
	TokenQuote
	TokenEOF
)

type Token struct {
	Type  TokenType
	Value string
}

type Parser struct {
	lexer *Lexer
	field string
}

func parse(input string, field string) (bleveQuery.Query, []string, error) {
	lexer := NewLexer(input)
	p := &Parser{
		lexer: lexer,
	}

	var exactMatches []string
	var reusableCurrentMatch strings.Builder
	var currentExactMatch *strings.Builder
	var currentWords []string
	var negated bool
	var parents []bleveQuery.Query
	var parentOps []TokenType
	var lastOp TokenType = TokenAND

	curr := bleve.NewBooleanQuery()

	for {
		token := p.lexer.NextToken()

		if token.Type == TokenEOF {
			if len(currentWords) > 0 {
				match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
				match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				match.SetField(field)
				if negated {
					curr.AddMustNot(match)
				} else {
					curr.AddMust(match)
				}
			}
			break
		}

		if token.Type == TokenQuote {
			if currentExactMatch == nil {
				currentExactMatch = &reusableCurrentMatch
			} else {
				exactMatches = append(exactMatches, currentExactMatch.String())
				currentExactMatch.Reset()
				reusableCurrentMatch = *currentExactMatch
				currentExactMatch = nil
			}
			continue
		}

		if currentExactMatch != nil {
			if currentExactMatch.Len() > 0 {
				currentExactMatch.WriteByte(' ')
			}
			currentExactMatch.WriteString(strings.ToLower(token.Value))
			currentWords = append(currentWords, token.Value)
			continue
		}

		if token.Type == TokenWord {
			currentWords = append(currentWords, token.Value)
			continue
		} else if len(currentWords) > 0 {
			match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
			match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
			match.SetField(field)
			if negated {
				curr.AddMustNot(match)
			} else {
				curr.AddMust(match)
			}
			currentWords = currentWords[:0]
			negated = false
		}

		switch token.Type {
		case TokenLParen:
			parents = append(parents, curr)
			parentOps = append(parentOps, lastOp)
			lastOp = TokenAND
			curr = bleve.NewBooleanQuery()
			continue
		case TokenRParen:
			if len(currentWords) > 0 {
				match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
				match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				match.SetField(field)
				if negated {
					curr.AddMustNot(match)
				} else {
					curr.AddMust(match)
				}
				currentWords = currentWords[:0]
				negated = false
			}

			if len(parents) > 0 {
				parent := parents[len(parents)-1]
				op := parentOps[len(parentOps)-1]

				var combined bleveQuery.Query
				switch op {
				case TokenOR:
					or := bleve.NewDisjunctionQuery()
					or.AddQuery(parent)
					or.AddQuery(curr)
					combined = or
				case TokenAND:
					and := bleve.NewConjunctionQuery()
					and.AddQuery(parent)
					and.AddQuery(curr)
					combined = and
				}

				curr = bleve.NewBooleanQuery()
				curr.AddMust(combined)
				parents = parents[:len(parents)-1]
				parentOps = parentOps[:len(parentOps)-1]
			}
			continue
		}

		next := p.lexer.NextToken()
		following := p.lexer.PeekToken()
		if next.Type == TokenNOT {
			negated = true
		}

		switch token.Type {
		case TokenOR:
			if next.Type != TokenLParen && !(next.Type == TokenNOT && following.Type == TokenLParen) {
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				or := bleve.NewDisjunctionQuery()
				or.AddQuery(curr)
				or.AddQuery(other)
				curr = bleve.NewBooleanQuery()
				curr.AddMust(or)
			} else {
				lastOp = TokenOR
			}
		case TokenAND:
			if next.Type != TokenLParen && !(next.Type == TokenNOT && following.Type == TokenLParen) {
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				and := bleve.NewConjunctionQuery()
				and.AddQuery(curr)
				and.AddQuery(other)
				curr = bleve.NewBooleanQuery()
				curr.AddMust(and)
			} else {
				lastOp = TokenAND
			}
		case TokenNOT:
			if next.Type != TokenLParen {
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				curr.AddMustNot(other)
			} else {
				negated = true
			}
		default:
			p.lexer.ReturnToken(next)
		}
	}

	return curr, exactMatches, nil
}
